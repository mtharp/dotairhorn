//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package main

import (
	"log"
	"math/rand"
	"strings"
	"unicode"
)

var soundmap = make(map[string][]string)

func normalize(v string) string {
	var b strings.Builder
	for _, r := range v {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func soundmapAdd(word, filename string) {
	word = normalize(word)
	existing := soundmap[word]
	for _, ex := range existing {
		if ex == filename {
			return
		}
	}
	soundmap[word] = append(existing, filename)
}

func soundmapFind(words []string) string {
	candidates := make(map[string]int)
	for _, wantpart := range words {
		for _, filename := range soundmap[normalize(wantpart)] {
			candidates[filename]++
		}
	}
	var final []string
	for filename, hits := range candidates {
		if hits == len(words) {
			final = append(final, filename)
		}
	}
	if len(final) == 0 {
		return ""
	}
	log.Printf("candidates: %s", strings.Join(final, " "))
	n := rand.Int() % len(final)
	return final[n]
}
