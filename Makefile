BINDIR=${CURDIR}/bin
APPNAME=worckerController

.PHONY: build
build:
	go build -o ${BINDIR}/${APPNAME} -v ./cmd/${APPNAME}/main.go
	$(eval NEW_VER:=$(shell cat version | cut -d '_' -f 2 ))
	mv ${BINDIR}/${APPNAME} ${BINDIR}/${APPNAME}_$(NEW_VER)

test:
	$(eval NEW_VER:=$(shell cat version | cut -d '_' -f 2 ))
	echo $(NEW_VER)
build-w:
	GOOS=windows GOARCH=amd64 go build -o  ${BINDIR}/${APPNAME}.exe ./cmd/${APPNAME}/main.go
	$(eval NEW_VER:=$(shell cat version | cut -d '_' -f 2 ))
	mv ${BINDIR}/${APPNAME}.exe ${BINDIR}/${APPNAME}_$(NEW_VER).exe

.DEFAULT_GOAL := build
