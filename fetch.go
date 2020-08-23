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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"eaglesong.dev/dvoice"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	dotaBase = "https://dota2.gamepedia.com/"
	tfBase   = "https://wiki.teamfortress.com/wiki/"
)

var (
	transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          1,
		IdleConnTimeout:       10 * time.Second,
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

func findMedia(baseURL, filename string) (string, error) {
	uppercased := strings.ToUpper(filename[:1]) + filename[1:]
	fileURL := baseURL + "File:" + uppercased
	page, err := grab(fileURL)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(baseURL)
	t := html.NewTokenizer(bytes.NewReader(page))
	for t.Next() != html.ErrorToken {
		tok := t.Token()
		if tok.Type != html.StartTagToken || tok.DataAtom != atom.Audio {
			continue
		}
		for _, attr := range tok.Attr {
			if attr.Key != "src" {
				continue
			}
			u, err = u.Parse(attr.Val)
			if err != nil {
				return "", err
			}
			return u.String(), nil
		}
	}
	return "", fmt.Errorf("no audio tag found in %s", fileURL)
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

	mp3URL, err := findMedia(baseURL, filename)
	if err != nil {
		return nil, err
	}
	mp3, err := grab(mp3URL)
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
	err = dvoice.Convert(context.Background(), ch, bytes.NewReader(mp3), bitrate)
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
