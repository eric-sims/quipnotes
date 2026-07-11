# Quipnotes

This project was inspired by a combination of two popular games: ["Ransom Notes" by Very Special Games](https://www.veryspecialgames.com/products/ransom-notes-the-ridiculous-word-magnet-game) 
and ["Quiplash" by Jackbox Games](https://www.jackboxgames.com/games/quiplash). Therefore, the combination of these two game names birthed a new name: Quipnotes!

The best way to experience this game is to buy "Ransom Notes" and use the prompt cards located there. 

## The Problem

My family enjoys playing "Quiplash" at parties because of its ease of use and ability for fun! Later, 
we started playing "Ransom Notes" which is also loads of fun! However, "Ransom Notes" is not as easy 
to play because it requires sitting around a table or hard surface. If you don't do this, you'll risk 
losing the _tiny_ word tiles that are crucial for this game.

Therefore, I started this project! This aims to make "Ransom Notes" as easy to play as any other JackBox-like
games.

## How to start

### Environment Setup
1. Copy `.env.example` to `.env`.
```
cp .env.example .env
```
2. Fill in the actual values in the `.env` file.

### Populate a `words.csv` file
The "Ransom Notes" games words are proprietary information. Please buy a copy and index those words (this only took 
me 15 minutes with all the tiles still in sheets). Or, feel free to use whatever word list you would like!

1. Put the words list in a csv file in one column. No title.
2. Save it somewhere and update the `WORDS_FILE_PATH` environment variable in your .env file.

### Populate a `prompts.txt` file (optional)
Prompts are the round cards a player writes a ransom note about. Like the words, the "Ransom Notes" prompt
cards are proprietary — supply your own list, or skip this step entirely: if `PROMPTS_FILE_PATH` is unset or
unreadable the server falls back to a small built-in bank so it still boots with a playable game.

1. Put one prompt per line in a plain text file (line-based, not CSV, so prompts may contain commas).
2. Blank lines are ignored, and a line starting with `#` is a comment — handy for a header.
3. **Family-friendly mode:** prefix a prompt with the `[adult]` marker to rate it *adult* (overly explicit,
   suggestive, or sexual). Adult prompts are withheld from games the host starts in family-friendly mode
   (a toggle on the manager screen), and the marker is stripped from the prompt text. Every other line is
   treated as family-friendly.
4. Save it somewhere and update the `PROMPTS_FILE_PATH` environment variable in your .env file.

See [`data/prompts.example.txt`](data/prompts.example.txt) for a ready-to-copy example.

```
# A comment. Everything below is a prompt, one per line.
The worst possible thing to say on a first date
A terrible name for a boat
[adult] Whisper something seductive to your date during a movie
```

## TODO
- [ ] Finish Client
- [ ] Create a user-friendly interaction for reading the ransom notes (another client?)
- [ ] (Extra) Add a way for users to add custom words to the word list if desired.
