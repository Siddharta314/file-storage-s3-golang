package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
)


type ffprobeOutput struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

func getVideoAspectRatio(filePath string) (string, error){
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	// output, err := cmd.Output()
	var output bytes.Buffer
    cmd.Stdout = &output
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	var data ffprobeOutput
	if err := json.Unmarshal(output.Bytes(), &data); err != nil {
		return "", err
	}
	if len(data.Streams) == 0 {
		return "", fmt.Errorf("no video streams found")
	}
	width := data.Streams[0].Width
	height := data.Streams[0].Height
	// if width*9 == height*16 {
	// 	return "16:9", nil
	// }
	// if width*16 == height*9 {
	// 	return "9:16", nil
	// }
	// using tolerance:
	ratio := float64(width) / float64(height)

    target169 := 16.0 / 9.0
    target916 := 9.0 / 16.0
    tolerance := 0.01

    if math.Abs(ratio - target169) < tolerance {
        return "16:9", nil
    }
    if math.Abs(ratio - target916) < tolerance {
        return "9:16", nil
    }
	return "other", nil
}
