VERSION = 1.0.0

all: build

build:
	go build -o godraw .

install: build
	mv ./godraw /usr/local/bin/godraw
