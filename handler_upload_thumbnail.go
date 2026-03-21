package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
    videoID, err := uuid.Parse(videoIDString)
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Invalid video ID format", err)
        return
    }

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// 	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	maxMemory := int64(10 << 20)
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing multipart form", err)
		return
	}
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting thumbnail file", err)
		return
	}
	defer file.Close()


	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
        return
    }

	fileData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error reading thumbnail file", err)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
        respondWithError(w, http.StatusBadRequest, "Invalid file type: only PNG and JPEG are allowed", nil)
        return
    }
	ext := "jpg"
	if mediaType == "image/png" {
        ext = "png"
    }

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of this video", nil)
		return
	}

	var buf [32]byte
    if _, err := rand.Read(buf[:]); err != nil {
        respondWithError(w, http.StatusInternalServerError, "Error generating random bytes", err)
        return
    }
	base64Token := base64.RawURLEncoding.EncodeToString(buf[:])
    fileName := fmt.Sprintf("%s.%s", base64Token, ext)
	filePath := filepath.Join(cfg.assetsRoot, fileName)
	fmt.Println("fileName:", fileName)

	dst, err := os.Create(filePath)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Unable to create file on disk", err)
        return
    }
    defer dst.Close()

	if _, err := io.Copy(dst, bytes.NewReader(fileData)); err != nil {
        respondWithError(w, http.StatusInternalServerError, "Error saving file data", err)
        return
    }
	thumbURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, fileName)
    video.ThumbnailURL = &thumbURL

    err = cfg.db.UpdateVideo(video)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Error updating video in DB", err)
        return
    }

	respondWithJSON(w, http.StatusOK, video)
}
