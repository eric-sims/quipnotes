# Quipnotes

This project was inspired by the hit game called ["Ransom Notes" by Very Special Games](https://www.veryspecialgames.com/products/ransom-notes-the-ridiculous-word-magnet-game). The best way to experience
this game is to buy the original and use the prompt cards located there. 

## The Problem

My family loves the game ["Quiplash" by Jackbox Games](https://www.jackboxgames.com/games/quiplash) because it is easy 
to play. Also, we can enjoy it from the comfort of our living room! We've recently discovered "Ransom Notes" and also 
love it! But we wanted to enjoy the same level of convenience that "Quiplash" offers. Therefore, I thought of putting 
together my skills to make this!

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

## Clients
quipNotes serves a static HTML page that communicates with the server. Please fill in the `HTML_DIR_PATH` in your
.env file. This gives you the option to make your own client or expand upon mine. :) 

## TODO
- [ ] Finish Client
- [ ] Create a user-friendly interaction for reading the ransom notes (another client?)
- [ ] (Extra) Add a way for users to add custom words to the word list if desired.
