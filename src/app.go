package main

import (
    "database/sql"
	"errors"
    "fmt"
    "image"
    "image/color"
    "image/jpeg"
	"io/ioutil"
    "log"
	"math"
    "net/http"
    "os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
    "github.com/disintegration/imaging"
    _ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

func main() {
    // Connect to the MariaDB database
    db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:3306)/%s", getDbUsername(), getDbPassword(), getDbHost(), getDbTable()))
    if err != nil {
		log.Fatal(fmt.Sprintf("Failure connecting to database (%v)", err))
    }

	db.SetMaxOpenConns(64)
	db.SetMaxIdleConns(6)
	db.SetConnMaxIdleTime(5 * time.Minute)

    defer db.Close()

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

    // Look up the photo information from the database
    filePath, rotation, _, err := lookupPhotoInfo(id)
    if err != nil {
        http.Error(w, fmt.Sprintf("%v", err), http.StatusNotFound)
        return
    }

	// TODO: return from cache, modified == _

    file, err := os.Open(filePath)
    if err != nil {
		log.Printf("failed to open file %s: %v", filePath, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
    }
    defer file.Close()

    w.Header().Set("Content-Type", "image/jpeg")

    if params["action"] == "download" {
		filename := params["name"]
		if filename != "" {
			filename = fmt.Sprintf("photo-%s.jpg", uuid.New().String())
		}
	    w.Header().Set("Content-Disposition", "attachment; filename=" + filename)
	}

	factor, boxWidth, boxHeight := getTransforms(params["size"])
	if factor != 0 || (boxWidth != 0 && boxHeight != 0) {
		img, err := transformPhoto(file, factor, boxWidth, boxHeight, rotation * -1)
		if err != nil {
			http.Error(w, fmt.Sprintf("%v", err), http.StatusInternalServerError)
			return
		}

		err = jpeg.Encode(w, img, nil)
		if err != nil {
		    log.Printf("failed to encode image as JPEG: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// TODO: Cache

	} else {
    	img, err := ioutil.ReadAll(file)
	    if err != nil {
			log.Printf("failed to read file %s: %v", filePath, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(img)))

		_, err = w.Write(img)
		if err != nil {
			log.Printf("failed to write response %s: %v", filePath, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// TODO: Cache
		
	}
}

func transformPhoto(file *os.File, factor int, boxWidth int, boxHeight int, rotation int) (image.Image, error) {
	img, _, err := image.Decode(file)
	if err != nil {
		log.Printf("failed to decode image from file %s: %v", file.Name(), err)
		return nil, errors.New("Internal server error")
	}

	bounds := img.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	var targetWidth int
	var targetHeight int
	if factor != 0 {
		targetWidth = origWidth / factor
		targetHeight = origHeight / factor
	} else if (boxWidth != 0 && boxHeight != 0) {

		rotatedWidth := origWidth
		rotatedHeight := origHeight
		if rotation == 90 || rotation == -90 || rotation == -270 {
			rotatedWidth = origHeight
			rotatedHeight = origWidth
		}

		ratioWidth := rotatedWidth / boxWidth
		ratioHeight := rotatedHeight / boxHeight
		ratio := math.Max(float64(ratioWidth), float64(ratioHeight))

		targetWidth = rotatedWidth / int(ratio)
		targetHeight = rotatedHeight / int(ratio)
	}

	// TODO: Try other algos for performance
	img = imaging.Resize(img, targetWidth, targetHeight, imaging.Lanczos)

	if (rotation != 0) {
		img = imaging.Rotate(img, float64(rotation), color.Black)
	}

	return img, nil
}

func lookupPhotoInfo(id int) (string, int, string, error) {
    // Query the database to retrieve the file path for the given ID
    var path string
	var rotation int
	var modified string
    err := db.QueryRow(
		"SELECT path, rotation, modified_timestamp " +
		"FROM photos " +
		"WHERE id_photo = ?",
		id).Scan(&path, &rotation, &modified)
    if err != nil {
		log.Printf("failed to find photo with id %s: %v", id, err)
        return "", 0, "", errors.New(fmt.Sprintf("failed to find photo with id %s", id))
    }

	filePath := "/mnt/photos/" + path
	_, err = os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("photo file does not exist (%v)", err)
			return "", 0, "", errors.New(fmt.Sprintf("failed to find photo with id %s", id))
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

func getParamsFromUrl(r *http.Request) map[string]string {
    // Retrieve the ID from the URL path
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
		return 0, errors.New(fmt.Sprintf("url contained an invalid id value (%s)", strId))
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

func getEnvDefault(key string, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		val = defaultVal
	}
	return val;
}

