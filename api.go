package main

import (
	"encoding/base64"
	"encoding/json"
	"log"

	"github.com/go-resty/resty/v2"
)

type Stream struct {
	Items []struct {
		Path string `json:"path"`
	} `json:"items"`
}

func Find() *Stream {
	client := resty.New()
	resp, _ := client.R().
		SetHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(Config.Ingest.Username+":"+Config.Ingest.Password))).
		Get("https://" + Config.Ingest.Hostname + "/mediamtx/v3/hlsmuxers/list")

	statusCode := resp.StatusCode()
	if statusCode >= 400 {
		log.Printf("Unexpected status code, got %d %s", statusCode, string(resp.Body()))
		return nil
	}

	var streams Stream
	err := json.Unmarshal(resp.Body(), &streams)
	if err != nil {
		log.Printf("Unmarshal Error %v", err)
		return nil
	}

	return &streams
}
