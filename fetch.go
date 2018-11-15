package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mtharp/dotairhorn/dvoice"
)

const (
	dotaBase = "https://dota2.gamepedia.com/"
	tfBase   = "https://wiki.teamfortress.com/wiki/"
)

var (
	mediaRe   = regexp.MustCompile(`(?:src|href)=["']([^"']+/[^"'/:]+(?:mp3|wav|aac))["']`)
	transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	cli = &http.Client{Transport: transport}
)

func grab(url string) ([]byte, error) {
	log.Printf("Fetching %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %s fetching %s", resp.Status, resp.Request.URL)
	}
	return ioutil.ReadAll(resp.Body)
}

func fetchSound(baseURL, filename string) ([][]byte, error) {
	cachePath := filepath.Join("cache", filename+".opus")
	f, err := os.Open(cachePath)
	if err == nil {
		defer f.Close()
		d := gob.NewDecoder(f)
		var frameList [][]byte
		if err := d.Decode(&frameList); err != nil {
			return nil, err
		}
		return frameList, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	page, err := grab(baseURL + "File:" + filename)
	if err != nil {
		return nil, err
	}
	matches := mediaRe.FindSubmatch(page)
	if len(matches) < 2 {
		return nil, fmt.Errorf("failed to extract media URL from %s", filename)
	}
	url := string(matches[1])

	mp3, err := grab(url)
	if err != nil {
		return nil, err
	}

	var frameList [][]byte
	ch := make(chan []byte)
	done := make(chan struct{})
	go func() {
		for frame := range ch {
			frameList = append(frameList, frame)
		}
		close(done)
	}()
	err = dvoice.PlayStream(context.Background(), ch, bytes.NewReader(mp3), params)
	close(ch)
	<-done
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(path.Dir(cachePath), 0755); err != nil {
		return nil, err
	}
	f, err = os.Create(cachePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g := gob.NewEncoder(f)
	if err := g.Encode(frameList); err != nil {
		return nil, err
	}
	return frameList, err
}
