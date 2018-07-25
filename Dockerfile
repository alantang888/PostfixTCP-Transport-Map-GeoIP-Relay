FROM golang:1.10
MAINTAINER Alan Tang

WORKDIR /go/src/app
COPY GeoIpTransportMap.go .

RUN wget -q http://geolite.maxmind.com/download/geoip/database/GeoLite2-Country.tar.gz && \
    tar -zxf GeoLite2-Country.tar.gz && \
    mv GeoLite2-Country_*/GeoLite2-Country.mmdb . && \
    rm -rf GeoLite2-Country_* GeoLite2-Country.tar.gz


RUN go get -d -v ./...
RUN go install -v ./...

CMD ["app"]