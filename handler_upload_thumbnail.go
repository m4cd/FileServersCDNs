package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20
	fmt.Println(maxMemory)
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Multipart parsing error", err)
		return
	}

	file, fileHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while geting image data from form", err)
		return
	}
	mediaType := fileHeader.Header.Get("Content-Type")
	imageData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while reading data", err)
		return
	}
	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized access to th e video", err)
		return
	}
	thumbnail := thumbnail{
		data:      imageData,
		mediaType: mediaType,
	}

	thumbnail_dataurl := "data:" + thumbnail.mediaType + ";base64,"
	thumbnail_dataurl = thumbnail_dataurl + base64.StdEncoding.EncodeToString(thumbnail.data)

	fmt.Println(thumbnail_dataurl)
	// videoThumbnails[videoID] = thumbnail
	// url := fmt.Sprintf("http://localhost:%v/api/thumbnails/%s", 8091, videoIDString)
	// dbVideo.ThumbnailURL = &url
	dbVideo.ThumbnailURL = &thumbnail_dataurl
	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error updating video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}
