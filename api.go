package main

import (
	"encoding/base64"
	"encoding/json"
	"log"

	"github.com/go-resty/resty/v2"
)

type Response struct {
	Items []Stream `json:"items"`
}

type Stream struct {
	Path string `json:"path"`
}

func Find() []Stream {
	client := resty.New()

	var protocol string
	if Config.Ingest.UseHttps {
		protocol = "https://"
	} else {
		protocol = "http://"
	}

	resp, _ := client.R().
		SetHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(Config.Ingest.Username+":"+Config.Ingest.Password))).
		Get(protocol + Config.Ingest.Hostname + "/mediamtx/v3/hlsmuxers/list")

	statusCode := resp.StatusCode()
	if statusCode >= 400 {
		log.Printf("Unexpected status code, got %d %s", statusCode, string(resp.Body()))
		return nil
	}

	var streams Response
	err := json.Unmarshal(resp.Body(), &streams)
	if err != nil {
		return nil
	}

	return streams.Items
}
