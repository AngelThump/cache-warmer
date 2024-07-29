package main

import (
	"bytes"
	"errors"
	"log"
	"regexp"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib/pkg/playlist"
	"github.com/go-resty/resty/v2"
)

var prevM3u8Bytes []byte

func main() {
	cfgPath, err := ParseFlags()
	if err != nil {
		log.Fatal(err)
	}
	err = NewConfig(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	getStreams()

	wg.Wait()
}

func getStreams() {
	streams := Find().Items
	if streams == nil {
		time.AfterFunc(300*time.Millisecond, func() {
			getStreams()
		})
		return
	}

	var wg sync.WaitGroup
	for _, stream := range streams {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			save(path)
		}(stream.Path)
	}
	wg.Wait()

	time.AfterFunc(300*time.Millisecond, func() {
		getStreams()
	})
}

func save(path string) {
	source := "https://" + Config.Ingest.Username + ":" + Config.Ingest.Password + "@" + Config.Ingest.Hostname + "/hls/" + path

	m3u8Bytes, err := get(source + "/stream.m3u8")
	if err != nil {
		return
	}

	if bytes.Equal(prevM3u8Bytes, m3u8Bytes) {
		log.Printf("[%s] M3u8 is the same. Ignore", path)
		return
	}

	log.Printf("[%s] Saving HLS", path)

	//Not the same, save new m3u8
	prevM3u8Bytes = m3u8Bytes

	//replace live path name as it is not used in redis
	regex := regexp.MustCompile(`live/`)
	stream := regex.ReplaceAllString(path, "hls/")

	go sendM3u8(m3u8Bytes, stream)
	pl, err := parseM3u8(m3u8Bytes)
	if err != nil {
		return
	}

	switch pl := pl.(type) {
	case *playlist.Media:
		go getAndSendInitMp4(pl.Map.URI, stream, source)

		for i := len(pl.Segments) - 1; i >= 0; i-- {
			seg := pl.Segments[i]
			if seg == nil {
				continue
			}
			go func(seg *playlist.MediaSegment, baseUrl string, source string) {
				getAndSendSegment(seg, baseUrl, source)
			}(seg, stream, source)
		}
	}

}
func getAndSendInitMp4(initMp4 string, path string, source string) error {
	//log.Printf("Saving %s", initMp4)
	client := resty.New()

	segmentBytes, err := get(source + "/" + initMp4)
	if err != nil {
		return err
	}

	redisBaseUrl := "http://" + Config.Redis.Hostname + "/" + path

	resp, _ := client.R().
		SetHeader("Authorization", "Bearer "+Config.Ingest.AuthKey).
		SetBody(segmentBytes).
		Post(redisBaseUrl + "/" + initMp4)

	statusCode := resp.StatusCode()
	if statusCode != 200 {
		log.Printf("Send Segment: Unexpected status code, got %d instead", statusCode)
		return errors.New(string(resp.Body()))
	}

	return nil
}

func get(url string) ([]byte, error) {
	client := resty.New()
	resp, _ := client.R().
		Get(url)

	statusCode := resp.StatusCode()
	if statusCode != 200 {
		log.Printf("Get Segment: Unexpected status code, got %d instead", statusCode)
		return nil, errors.New(string(resp.Body()))
	}

	return resp.Body(), nil
}

func parseM3u8(m3u8Bytes []byte) (playlist.Playlist, error) {
	pl, err := playlist.Unmarshal(m3u8Bytes)
	if err != nil {
		return nil, errors.New("Failed to decode m3u8..")
	}

	return pl, nil
}

func sendM3u8(m3u8Bytes []byte, path string) {
	client := resty.New()
	redisBaseUrl := "http://" + Config.Redis.Hostname + "/" + path

	resp, _ := client.R().
		SetHeader("Authorization", "Bearer "+Config.Ingest.AuthKey).
		SetBody(m3u8Bytes).
		Post(redisBaseUrl + "/index.m3u8")
	statusCode := resp.StatusCode()
	if statusCode != 200 {
		log.Printf("Send M3u8: Unexpected status code, got %d instead", statusCode)
		return
	}
}

func getAndSendSegment(seg *playlist.MediaSegment, path string, source string) error {
	//log.Printf("Saving %s", seg.URI)
	client := resty.New()

	segmentBytes, err := get(source + "/" + seg.URI)
	if err != nil {
		return err
	}

	redisBaseUrl := "http://" + Config.Redis.Hostname + "/" + path

	resp, _ := client.R().
		SetHeader("Authorization", "Bearer "+Config.Ingest.AuthKey).
		SetBody(segmentBytes).
		Post(redisBaseUrl + "/" + seg.URI)

	statusCode := resp.StatusCode()
	if statusCode != 200 {
		log.Printf("Send Segment: Unexpected status code, got %d instead", statusCode)
		return errors.New(string(resp.Body()))
	}

	return nil
}
