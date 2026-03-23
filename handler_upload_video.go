package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxVideoSize = 1 << 30 // 1 GB
	r.Body = http.MaxBytesReader(w, r.Body, maxVideoSize)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID format", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid JWT", err)
		return
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

	err = r.ParseMultipartForm(maxVideoSize)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing multipart form", err)
		return
	}
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting video file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type: only MP4 is allowed", nil)
		return
	}

	tmpFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temporary file", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()
	_, err = io.Copy(tmpFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error saving video to disk", err)
		return
	}

	_, err = tmpFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting file pointer", err)
		return
	}
	processedPath, err := processVideoForFastStart(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error processing video for fast start", err)
		return
	}
	defer os.Remove(processedPath)
	processedFile, err := os.Open(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not open processed file", err)
		return
	}
	defer processedFile.Close()

	// get aspect ratio
	aspectRatio, err := getVideoAspectRatio(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting video aspect ratio", err)
		return
	}
	aspectPrefix := "other"
	if aspectRatio == "16:9" {
		aspectPrefix = "landscape"
	} else if aspectRatio == "9:16" {
		aspectPrefix = "portrait"
	}
	// PutObject
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error generating random name", err)
		return
	}

	s3Key := fmt.Sprintf("%s/%x.mp4", aspectPrefix, randomBytes)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &s3Key,
		Body:        processedFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading to S3", err)
		return
	}

	// s3URL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, s3Key)
	//let's use presigned URLs
	bucketAndKey := fmt.Sprintf("%s,%s", cfg.s3Bucket, s3Key)
	video.VideoURL = &bucketAndKey
    err = cfg.db.UpdateVideo(video)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Error updating video in database", err)
        return
    }
	signedVideo, err := cfg.dbVideoToSignedVideo(video)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Error signing video URL", err)
        return
    }
	respondWithJSON(w, http.StatusOK, signedVideo)
	// video.VideoURL = &s3URL
    // err = cfg.db.UpdateVideo(video)
    // if err != nil {
    //     respondWithError(w, http.StatusInternalServerError, "Error updating video in database", err)
    //     return
    // }

    // respondWithJSON(w, http.StatusOK, video)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error){
	presignClient := s3.NewPresignClient(s3Client)
	presignedRequest, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
        Bucket: &bucket,
        Key:    &key,
    }, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return presignedRequest.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error){
	if video.VideoURL == nil {
        return video, nil
    }
	parts := strings.Split(*video.VideoURL, ",")
    if len(parts) != 2 {
        return video, nil 
    }
	bucket := parts[0]
	key := parts[1]
	presignedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, 1*time.Hour)
	if err != nil {
		return video, err
	}
	video.VideoURL = &presignedURL
	return video, nil
}