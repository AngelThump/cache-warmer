package main

import (
	"bytes"
	"errors"
	"log"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/bluenviron/gohlslib/pkg/playlist"
	"github.com/go-resty/resty/v2"
)

var currentStreams []Stream

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
	streams := Find()
	if streams == nil {
		time.AfterFunc(1000*time.Millisecond, func() {
			getStreams()
		})
		return
	}

	for _, stream := range streams {
		//Skip if already monitoring
		exists := slices.ContainsFunc(currentStreams, func(n Stream) bool {
			return n.Path == stream.Path
		})
		if exists {
			continue
		}

		currentStreams = append(currentStreams, stream)
		go Check(stream, nil)
	}

	//Delete if not streaming anymore
	for i, existingStream := range currentStreams {
		exists := slices.ContainsFunc(streams, func(n Stream) bool {
			return n.Path == existingStream.Path
		})
		if exists {
			continue
		}
		currentStreams = slices.Delete(currentStreams, i, i+1)
	}

	time.AfterFunc(1000*time.Millisecond, func() {
		getStreams()
	})
}

func Check(stream Stream, prevM3u8Bytes []byte) {
	streaming := slices.ContainsFunc(currentStreams, func(n Stream) bool {
		return n.Path == stream.Path
	})
	//Quit if no longer monitoring
	if !streaming {
		return
	}

	prevM3u8Bytes = save(stream, prevM3u8Bytes)

	time.AfterFunc(300*time.Millisecond, func() {
		Check(stream, prevM3u8Bytes)
	})
}

func save(stream Stream, prevM3u8Bytes []byte) []byte {
	path := stream.Path

	var protocol string
	if Config.Ingest.UseHttps {
		protocol = "https://"
	} else {
		protocol = "http://"
	}

	source := protocol + Config.Ingest.Username + ":" + Config.Ingest.Password + "@" + Config.Ingest.Hostname + "/hls/" + path

	m3u8Bytes, err := get(source + "/stream.m3u8")
	if err != nil {
		return prevM3u8Bytes
	}

	if bytes.Equal(prevM3u8Bytes, m3u8Bytes) {
		log.Printf("[%s] M3u8 is the same. Ignore", path)
		return prevM3u8Bytes
	}

	//log.Printf("[%s] Saving HLS", path)

	//Not the same, save new m3u8
	//replace live path name as it is not used in redis
	regex := regexp.MustCompile(`live/`)
	streamPath := regex.ReplaceAllString(path, "hls/")

	go sendM3u8(m3u8Bytes, streamPath)
	pl, err := parseM3u8(m3u8Bytes)
	if err != nil {
		return prevM3u8Bytes
	}

	switch pl := pl.(type) {
	case *playlist.Media:
		go getAndSendInitMp4(pl.Map.URI, streamPath, source)

		if prevM3u8Bytes == nil {
			for _, seg := range pl.Segments {
				if seg == nil {
					continue
				}
				go func(seg *playlist.MediaSegment, streamPath string, source string) {
					getAndSendSegment(seg, streamPath, source)
				}(seg, streamPath, source)
			}
		} else {
			seg := pl.Segments[len(pl.Segments)-1]
			go getAndSendSegment(seg, streamPath, source)
		}
	}
	return m3u8Bytes
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
		log.Printf("Get %s: Unexpected status code, got %d instead", url, statusCode)
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
	log.Printf("[%s] Saving %s", path, seg.URI)
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
