# vim: ts=4 sw=4 sts=4 expandtab
VERSION 0.6                                                                     
FROM golang:alpine
WORKDIR /build

deps:
	COPY go.mod go.sum ./
	RUN go mod download
	SAVE ARTIFACT go.sum AS LOCAL go.sum
	SAVE ARTIFACT go.mod AS LOCAL go.mod
	

build:                
    FROM +deps                                                          
    RUN apk --no-cache add tzdata
    COPY main.go ./main.go                                      
    RUN GOOS=linux CGO_ENABLED=0 go build -trimpath -o \
        /build/maintenance-exporter main.go                                    
    SAVE ARTIFACT /build/maintenance-exporter AS LOCAL maintenance-exporter     
    SAVE ARTIFACT /usr/share/zoneinfo /zoneinfo

docker:
    FROM scratch
    COPY +build/zoneinfo /usr/share/zoneinfo
    COPY +build/maintenance-exporter /app/maintenance-exporter
    ENTRYPOINT ["/app/maintenance-exporter"]
    SAVE IMAGE goestin/maintenance-exporter:v0.2.0

