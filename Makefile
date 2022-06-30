GOCMD=go
GOBUILD=$(GOCMD) build
GOHOSTOS=$(strip $(shell $(GOCMD) env get GOHOSTOS))

TAG ?= $(shell git describe --tags)
COMMIT ?= $(shell git describe --always)
BUILD_DATE ?= $(shell date -u +%m/%d/%Y)


STALKER=bin/stalker

all: target

clean:
	rm -rf ${STALKER} 

target:
	GOARCH=amd64 GOOS=linux $(GOBUILD) -ldflags "-X main.version=$(TAG) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)" -o ${STALKER} github.com/zawachte/stalker

influxd:
	wget https://dl.influxdata.com/influxdb/releases/influxdb2-2.2.0-linux-amd64.tar.gz
	tar xvzf influxdb2-2.2.0-linux-amd64.tar.gz
	cp influxdb2-2.2.0-linux-amd64/influxd bin/
	rm -r influxdb2-2.2.0-linux-amd64
	rm influxdb2-2.2.0-linux-amd64.tar.gz


