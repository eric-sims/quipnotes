package internal

import (
	"slices"

	"github.com/gorilla/websocket"
)

type Player struct {
	ID         string
	Conn       *websocket.Conn
	wordsDrawn []string
}

func (p *Player) AddWords(words []string) {
	p.wordsDrawn = append(p.wordsDrawn, words...)
}

func (p *Player) RemoveWords(words []string) {
	for _, word := range words {
		p.RemoveWord(word)
	}
}

func (p *Player) RemoveWord(s string) {
	// Find the index of the string to delete
	index := -1
	for i, w := range p.wordsDrawn {
		if w == s {
			index = i
			break
		}
	}

	// Delete the string if found
	if index != -1 {
		p.wordsDrawn = slices.Delete(p.wordsDrawn, index, index+1)
	}
}
