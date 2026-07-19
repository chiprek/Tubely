package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func getVideoAspectRatio(filePath string) (string, error) {

	var buffer bytes.Buffer
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmd.Stdout = &buffer

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("could not run ffprobe: %w", err)
	}

	var probe struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(buffer.Bytes(), &probe); err != nil {
		return "", fmt.Errorf("could not parse ffprobe output: %w", err)
	}

	if len(probe.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	width := probe.Streams[0].Width
	height := probe.Streams[0].Height

	const tolerance = 0.1
	ratio := float64(width) / float64(height)

	switch {
	case math.Abs(ratio-16.0/9.0) < tolerance:
		return "16:9", nil
	case math.Abs(ratio-9.0/16.0) < tolerance:
		return "9:16", nil
	default:
		return "other", nil
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("could not run ffmpeg: %w", err)
	}
	return outputFilePath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s3Client)
	req, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("could not presign request: %w", err)
	}
	return req.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}

	parts := strings.SplitN(*video.VideoURL, ",", 2)
	if len(parts) != 2 {
		return database.Video{}, fmt.Errorf("invalid video url: %s", *video.VideoURL)
	}
	bucket, key := parts[0], parts[1]

	signedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, 15*time.Minute)
	if err != nil {
		return database.Video{}, fmt.Errorf("could not generate presigned url: %w", err)
	}

	video.VideoURL = &signedURL
	return video, nil
}
