package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
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

	faststartFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to process the file", err)
		return
	}

	defer os.Remove(faststartFilePath)

	faststartFile, err := os.Open(faststartFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to open processed file", err)
		return
	}
	defer faststartFile.Close()

	tempFile.Seek(0, io.SeekStart)

	ratio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getVideoAspectRatio", err)
		return
	}
	prefix := aspectRatioToPrefix[ratio]

	b := make([]byte, 32)
	rand.Read(b)
	filenameBytes := make([]byte, base64.RawURLEncoding.EncodedLen(len(b)))
	base64.RawURLEncoding.Encode(filenameBytes, b)
	fileName := prefix + string(filenameBytes) + "." + extension

	putObjectInput := &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(fileName),
		Body:        faststartFile,
		ContentType: aws.String("video/mp4"),
	}

	_, err = cfg.s3Client.PutObject(ctx, putObjectInput)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error putting object on S3 bucket", err)
		return
	}
	videoURL := "https://" + cfg.s3CfDistribution + "/" + fileName
	fmt.Println(videoURL)
	// videoURL := cfg.s3Bucket + "," + fileName
	dbVideo.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error updating video metadata", err)
		return
	}
	// dbVideo, err = cfg.dbVideoToSignedVideo(dbVideo)
	// if err != nil {
	// 	respondWithError(w, http.StatusInternalServerError, "Error in dbVideoToSignedVideo", err)
	// 	return
	// }
	
	respondWithJSON(w, http.StatusOK, dbVideo)
}

func getVideoAspectRatio(filePath string) (string, error) {
	command := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var commandOutputBuffer bytes.Buffer
	command.Stdout = &commandOutputBuffer
	command.Run()

	var results FFProbeResult
	err := json.Unmarshal(commandOutputBuffer.Bytes(), &results)
	if err != nil {
		return "Error", err
	}

	if len(results.Streams) == 0 {
		return "Error", fmt.Errorf("no streams found")
	}

	width := results.Streams[0].Width
	height := results.Streams[0].Height

	if width/height == 1 {
		return "16:9", nil
	}
	if width/height == 0 {
		return "9:16", nil
	}

	return "other", nil

}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"
	command := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()

	if err != nil {
		return "Error", err
	}

	return outputFilePath, nil
}

// func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
// 	s3PresignedClient := s3.NewPresignClient(s3Client)
// 	req, err := s3PresignedClient.PresignGetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}, func(opts *s3.PresignOptions) { opts.Expires = expireTime })
// 	if err != nil {
// 		return "Error in generatePresignedURL", err
// 	}

// 	return req.URL, nil
// }

// func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
// 	fmt.Println(video.VideoURL)
// 	splt := strings.Split(*video.VideoURL, ",")
// 	bucket := splt[0]
// 	key := splt[1]
// 	url, err := generatePresignedURL(cfg.s3Client, bucket, key, 365*24*3600*time.Second)
// 	if err != nil {
// 		return video, err
// 	}
// 	video.VideoURL = &url
// 	return video, nil
// }
