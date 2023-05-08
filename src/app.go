package main

import (
    "crypto/md5"
    "database/sql"
    "errors"
    "fmt"
    "io"
    "log"
    "math"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"

    "github.com/google/uuid"
    "github.com/davidbyttow/govips/v2/vips"
    _ "github.com/go-sql-driver/mysql"
)

var db *sql.DB
var lfuCache map[string][]byte
var lfuCacheCounts map[string]int

func main() {
    var err error

    lfuCache = make(map[string][]byte)
    lfuCacheCounts = make(map[string]int)

    db, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:3306)/%s", getDbUsername(), getDbPassword(), getDbHost(), getDbTable()))
    if err != nil {
        log.Fatalf("Failure connecting to database (%v)", err)
    }

    db.SetMaxOpenConns(64)
    db.SetMaxIdleConns(6)
    db.SetConnMaxIdleTime(5 * time.Minute)

    defer db.Close()

    vips.Startup(&vips.Config{ ConcurrencyLevel : getConcurrencyLevel() })
    defer vips.Shutdown()

    http.HandleFunc(getRootContext(), getPhoto)

    log.Fatal(http.ListenAndServe(":8080", nil))
}

func getPhoto(w http.ResponseWriter, r *http.Request) {
    params := getParamsFromUrl(r)

    id, err := getIdParam(params)
    if err != nil {
        http.Error(w, fmt.Sprintf("%v", err), http.StatusBadRequest)
        return
    }

    filePath, rotation, modified, err := lookupPhotoInfo(id)
    if err != nil {
        http.Error(w, fmt.Sprintf("%v", err), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "image/jpeg")

    if params["action"] == "download" {
        filename := params["name"]
        if filename != "" {
            filename = fmt.Sprintf("photo-%s.jpg", uuid.New().String())
        }
        w.Header().Set("Content-Disposition", "attachment; filename=" + filename)
    }

    var imgBytes []byte

    size := params["size"]
    factor, boxWidth, boxHeight := getTransforms(size)
    if factor != 0 || (boxWidth != 0 && boxHeight != 0) {

        imgBytes = getCachedPhoto(filePath, rotation, modified, size)
        if len(imgBytes) == 0 {
            imgBytes, err = transformPhotoThumbnail(filePath, factor, boxWidth, boxHeight, rotation)
            if err != nil {
                http.Error(w, fmt.Sprintf("%v", err), http.StatusInternalServerError)
                return
            }

            putCachedPhoto(filePath, rotation, modified, size, imgBytes)
        }
    } else {
        file, err := os.Open(filePath)
        if err != nil {
            log.Printf("failed to open file %s: %v", filePath, err)
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }
        defer file.Close()

        imgBytes, err = io.ReadAll(file)
        if err != nil {
            log.Printf("failed to read file %s: %v", filePath, err)
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }
    }

    w.Header().Set("Content-Length", strconv.Itoa(len(imgBytes)))

    _, err = w.Write(imgBytes)
    if err != nil {
        log.Printf("failed to write response %s: %v", filePath, err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }
}

func transformPhotoThumbnail(filePath string, factor int, boxWidth int, boxHeight int, rotation int) ([]byte, error) {
    if rotation != 0 {
        log.Printf("Rotation: %d, file %s", rotation, filePath)
    }

    var err error
    var img *vips.ImageRef
    if factor != 0 {
        img, err = vips.NewImageFromFile(filePath)
        if err != nil {
            log.Printf("failed to open file %s: %v", filePath, err)
            return nil, errors.New("internal server error")
        }
        defer img.Close()

        err = img.Resize(float64(1) / float64(factor), vips.KernelNearest)
        if err != nil {
            log.Printf("failed to resize image %s: %v", filePath, err)
            return nil, errors.New("internal server error")
        }
    } else if (boxWidth != 0 && boxHeight != 0) {
        img, err = vips.NewThumbnailWithSizeFromFile(filePath, boxWidth, boxHeight, vips.InterestingNone, vips.SizeDown)
        if err != nil {
            log.Printf("failed to generate thumbnail from file %s: %v", filePath, err)
            return nil, errors.New("internal server error")
        }
        defer img.Close()
    }

    exportParams := vips.JpegExportParams {
        Quality: 80,
        Interlace: true,
        StripMetadata: true,
    }

    imgBytes, _, err := img.ExportJpeg(&exportParams)
    if err != nil {
        log.Printf("failed to export image for %s: %v", filePath, err)
        return nil, errors.New("internal server error")
    }

    return imgBytes, nil
}

func lookupPhotoInfo(id int) (string, int, string, error) {
    var path string
    var rotation int
    var modified string
    err := db.QueryRow(
        "SELECT path, rotation, modified_timestamp " +
        "FROM photos " +
        "WHERE id_photo = ?",
        id).Scan(&path, &rotation, &modified)
    if err != nil {
        log.Printf("failed to find photo with id %d: %v", id, err)
        return "", 0, "", fmt.Errorf("failed to find photo with id %d", id)
    }

    filePath := "/mnt/photos/" + path
    _, err = os.Stat(filePath)
    if err != nil {
        if os.IsNotExist(err) {
            log.Printf("photo file (%s) does not exist (%v)", filePath, err)
            return "", 0, "", fmt.Errorf("failed to find photo with id %d", id)
        }
    }

    return filePath, rotation, modified, nil
}

func getTransforms(size string) (int, int, int) {
    switch size {
    case "full":
        return 1, 0, 0
    case "half":
        return 2, 0, 0
    case "quarter":
        return 4, 0, 0
    case "eighth":
        return 8, 0, 0
    case "xsmall":
        return 0, 80, 80
    case "small":
        return 0, 160, 160
    case "medium":
        return 0, 320, 320
    case "large":
        return 0, 640, 480
    case "xlarge":
        return 0, 800, 600
    case "xxlarge":
        return 0, 1024, 768
    case "xxxlarge":
        return 0, 1280, 1024
    case "xxxxlarge":
        return 0, 1600, 1200
    case "tivo":
        return 0, 320, 320
    case "blog":
        return 0, 852, 852
    case "home":
        return 0, 990, 990
    default:
        return 0, 0, 0
    }
}

func getCachedPhoto(path string, rotation int, modified string, size string) []byte {
    if getLfuCacheMaxCount() == 0 {
        return []byte{}
    }

    key := getCachedPhotoKey(path, rotation, modified, size)

    buf := lfuCache[key]
    if len(buf) != 0 {
        lfuCacheCounts[key] += 1
    }

    return buf
}

func putCachedPhoto(path string, rotation int, modified string, size string, buf []byte) {
    if getLfuCacheMaxCount() == 0 {
        return
    }

    key := getCachedPhotoKey(path, rotation, modified, size)

    lfuCache[key] = buf
    lfuCacheCounts[key] = lfuCacheCounts[key] + 1

    if len(lfuCache) > getLfuCacheMaxCount() {
        minUsed := math.MaxInt32
        minUsedKey := ""
        for ckey := range lfuCache {
            if ckey != key && lfuCacheCounts[ckey] < minUsed {
                minUsed = lfuCacheCounts[ckey]
                minUsedKey = ckey
            }
        }

        if minUsedKey != "" {
            delete(lfuCache, minUsedKey)
            delete(lfuCacheCounts, minUsedKey)
        }
    }
}

func getCachedPhotoKey(path string, rotation int, modified string, size string) string {
    hash := md5.New()

    hash.Write([]byte(path))
    hash.Write([]byte(fmt.Sprintf("%d", rotation)))
    hash.Write([]byte(modified))
    hash.Write([]byte(size))

    return fmt.Sprintf("%x", hash.Sum(nil))
}

func getParamsFromUrl(r *http.Request) map[string]string {
    path := r.URL.Path[len(getRootContext()):]
    parts := strings.Split(path, "/")

    params := make(map[string]string)

    if len(parts) >= 2 {
        for i := 1; i < len(parts); i += 2 {
            params[parts[i-1]] = parts[i]
        }
    }

    return params
}

func getIdParam(params map[string]string) (int, error) {
    strId := params["id"]
    if strId == "" {
        log.Print("url did not contain id")
        return 0, errors.New("url did not contain id")
    }

    id, err := strconv.Atoi(strId)
    if err != nil {
        log.Printf("url contained an invalid id value (%s) (%v)", strId, err)
        return 0, fmt.Errorf("url contained an invalid id value (%s)", strId)
    }

    return id, nil
}

func getDbHost() string {
    return getEnvDefault("DB_HOST", "127.0.0.1")
}

func getDbTable() string {
    return getEnvDefault("DB_TABLE", "photos2")
}

func getDbUsername() string {
    return getEnvDefault("DB_USERNAME", "photos")
}

func getDbPassword() string {
    return getEnvDefault("DB_PASSWORD", "photos")
}

func getRootContext() string {
    return getEnvDefault("ROOT_CONTEXT", "/photos/photo/")
}

func getLfuCacheMaxCount() int {
    return getEnvAsIntDefault("LFU_CACHE_MAX_COUNT", 32)
}

func getConcurrencyLevel() int {
    return getEnvAsIntDefault("CONCURRENCY_LEVEL", 4)
}

func getEnvDefault(key string, defaultVal string) string {
    val := os.Getenv(key)
    if val == "" {
        return defaultVal
    } else {
        return val
    }
}

func getEnvAsIntDefault(key string, defaultVal int) int {
    strVal := os.Getenv(key)
    if strVal == "" {
        return defaultVal
    } else {
        val, err := strconv.Atoi(strVal)
        if err != nil {
            log.Printf("unable to convert env value (%s) for env key %s (%v)", strVal, key, err)
            return defaultVal
        } else {
            return val
        }
    }
}

