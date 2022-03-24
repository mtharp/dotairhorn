package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"os"
	"path"
	"path/filepath"

	"eaglesong.dev/dvoice"
	"dotairhorn/internal"
)

var baseURLs = map[string]string{
	"dota2": "https://dota2.fandom.com/",
	"tf2":   "https://wiki.teamfortress.com/w/",
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

	mp3URL, err := internal.MediaURL(baseURL, filename)
	if err != nil {
		return nil, err
	}
	mp3, err := internal.Grab(mp3URL)
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
