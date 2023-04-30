package main

import (
    "database/sql"
	"errors"
    "fmt"
    "image"
    "image/color"
    "image/jpeg"
    "log"
    "net/http"
    "os"
    "path/filepath"
	"strconv"
	"strings"
	"time"

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
    filePath, rotation, modified, err := lookupPhotoInfo(id)
    if err != nil {
        http.Error(w, fmt.Sprintf("%v", err), http.StatusNotFound)
        return
    }

	err = transformPhoto(filePath, rotation, modified, params["size"])
	if err != nil {
        http.Error(w, fmt.Sprintf("%v", err), http.StatusInternalServerError)
		return
	}
}

func transformPhoto(filePath string, rotation int, modified string, size string) (error) {
    // Open the file and decode it as an image
    file, err := os.Open(filePath)
    if err != nil {
		log.Printf("failed to open file %s: %v", filePath, err)
		return errors.New("Internal server error")
    }
    defer file.Close()

    img, _, err := image.Decode(file)
    if err != nil {
		log.Printf("failed to decode image from file %s: %v", filePath, err)
		return errors.New("Internal server error")
    }

    // Rotate the image by 90 degrees clockwise
    // rotatedImg := imaging.Rotate(img, 90, color.Black)

    // Resize the image to 300x300 pixels
    // resizedImg := resize.Resize(300, 300, rotatedImg, resize.Lanczos3)

    // Encode the resulting image in JPEG format
    // err = jpeg.Encode(w, rotatedImg, nil)
    // if err != nil {
    //     log.Printf("failed to encode image as JPEG: %v", err)
	// 	return errors.New("Internal server error")
    // }

	return nil
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

