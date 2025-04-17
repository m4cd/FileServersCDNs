package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
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
	dbVideo, err := cfg.db.GetVideo(videoID)

	if err != nil || dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized access to the video", err)
		return
	}

	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while geting video data from form", err)
		return
	}

	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while parsing mediatype", err)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusInternalServerError, "Error while parsing mediatype", err)
		return
	}
	extension := strings.Split(mediaType, "/")[1]

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Filesystem error ", err)
		return
	}

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to copy file", err)
		return
	}

	tempFile.Seek(0, io.SeekStart)

	b := make([]byte, 32)
	rand.Read(b)
	filenameBytes := make([]byte, base64.RawURLEncoding.EncodedLen(len(b)))
	base64.RawURLEncoding.Encode(filenameBytes, b)
	fileName := string(filenameBytes) + "." + extension

	putObjectInput := &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(fileName),
		Body:        tempFile,
		ContentType: aws.String("video/mp4"),
	}

	_, err = cfg.s3Client.PutObject(ctx, putObjectInput)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error putting object on S3 bucket", err)
		return
	}
	videoURL := "https://" + cfg.s3Bucket + ".s3." + cfg.s3Region + ".amazonaws.com/" + fileName
	dbVideo.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error updating video metadata", err)
		return
	}
	respondWithJSON(w, http.StatusOK, dbVideo)
}
