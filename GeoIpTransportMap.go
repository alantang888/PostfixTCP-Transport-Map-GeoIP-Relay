/*
   Copyright 2018 Alan Tang

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
	"io"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"
)

var destinationMap map[string][]string
var defaultTarget string

func init() {
	destinationMap = make(map[string][]string)

	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.InfoLevel)
}

func main() {
	app := argsParserSetup()

	// Args handling setup
	app.Action = argsHandler

	// Args parse
	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("Parse args error: ", err.Error())
	}

	// TODO: handle geoip db update
	listenInterface := "0.0.0.0"
	listenPort := "2527"

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%s", listenInterface, listenPort))
	if err != nil {
		log.Fatalf("Listen %s:%s error: %s", listenInterface, listenPort, err.Error())
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Errorf("Connection accept error for %v.", conn.RemoteAddr())
			continue
		}

		go handleConnection(conn)
	}

}

func argsParserSetup() *cli.App {
	app := cli.NewApp()
	app.Name = "GeoIpTransportMap"
	app.Usage = "make an explosive entrance"
	app.Flags = []cli.Flag{
		cli.StringSliceFlag{
			Name:  "target,t",
			Usage: `Target destination mapping. Format: "XX:MTA". XX=ISO alpha-2 Country code. MTA is nexthop MTA IP/Hostname.`,
			//EnvVar: "TARGET_MAPPING",
		},
		cli.StringFlag{
			Name:        "default,d",
			Usage:       "Default target. If country not in target mapping, use this default.",
			Destination: &defaultTarget,
		},
		cli.BoolFlag{
			Name:  "help,h",
			Usage: "Print this help.",
		},
	}
	app.HideVersion = true
	app.HideHelp = true

	return app
}

func argsHandler(c *cli.Context) error {
	needHelp := c.Bool("help")
	if needHelp {
		cli.ShowAppHelpAndExit(c, 1)
	}

	mapping := c.StringSlice("target")

	if len(mapping) < 1 {
		cli.ShowAppHelp(c)
		return errors.New("Can't process with empty target mapping.")
	}

	for _, value := range mapping {
		splitedMap := strings.Split(value, ":")
		if len(splitedMap) != 2 {
			return errors.New(fmt.Sprintf("Invalid mapping format: %s", value))
		}
		country := strings.ToUpper(splitedMap[0])
		if len(country) != 2 {

			return errors.New(fmt.Sprintf("Invalid country code: %s", country))
		}
		target := splitedMap[1]
		if len(target) < 1 {
			return errors.New(fmt.Sprintf("Invalid target on %s: %s", country, target))
		}

		destinationMap[country] = append(destinationMap[country], target)
	}

	defaultTarget = strings.ToUpper(defaultTarget)

	if _, ok := destinationMap[defaultTarget]; !ok {
		cli.ShowAppHelp(c)
		return errors.New(fmt.Sprintf(`Default target "%s" not in target map.`, defaultTarget))
	}
	return nil
}

func handleConnection(conn net.Conn) {
	log.Infof("Start handle connection '%v'.", conn.RemoteAddr())
	reader := bufio.NewReader(conn)
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				log.Infof("Connection closed from %v.", conn.RemoteAddr())
			} else {
				log.Errorf("Read from %v error: '%s'.", conn.RemoteAddr(), err.Error())
			}
			conn.Close()
			return
		}
		length := len(data)
		//if length < 1{
		dataString := string(data[:length-1])

		log.Infof("Received '%s'", dataString)

		result := getResult(dataString)
		conn.Write([]byte(genPostfixResponse(result)))
		log.Infof("Email %s use %s as next hop.", dataString, result)
	}
}

func getEmailDomain(email string) (string, error) {
	splitedEmail := strings.Split(email, "@")
	if len(splitedEmail) != 2 {
		errorMsg := fmt.Sprintf("Email address invalid: %v", email)
		log.Warnln(errorMsg)
		return "", errors.New(errorMsg)
	}

	return splitedEmail[1], nil
}

func getMx(domain string) ([]*net.MX, error) {
	// LookupMX will return a MX list sorted by priority. So no need to sort
	mxs, err := net.LookupMX(domain)

	if err != nil {
		log.Warnf("Get MX error on %v: %v", domain, err)
		return mxs, err
	}

	return mxs, err
}

func isIpv4(ip net.IP) bool {
	if ip.To4() == nil {
		return false
	}
	return true
}

func getIp(mx *net.MX) (net.IP, error) {
	ips, err := net.LookupIP(mx.Host)
	if err != nil {
		log.Warnf("Get IP error on %v: %v", mx.Host, err)
		return net.IP{}, errors.New(fmt.Sprint("Get IP error from MX record(s)."))
	}

	length := len(ips)
	switch {
	// TODO: handle IPv4/IPv6
	case length == 1:
		return ips[0], nil
	case length > 1:
		// Get a random IP from IP slice
		rand.Seed(time.Now().UnixNano())

		// TODO: need extra check for ip correct?
		return ips[rand.Intn(length)], nil
	}

	return net.IP{}, errors.New(fmt.Sprint("Can't get IP from \"%s\" MX record(s).", mx.Host))
}

func getCountryByIp(ipAddress net.IP) (string, error) {
	// TODO: reduce read file. should read from cache by geoip2.FromBytes()
	db, err := geoip2.Open("GeoLite2-Country.mmdb")
	if err != nil {
		log.Fatalf("Open GeoIP DB file error: %s", err.Error())
	}
	defer db.Close()

	record, err := db.Country(ipAddress)
	if err != nil {
		log.Warnf("Get country error on %v: %v", ipAddress.String(), err)
		return "", err
	}

	return record.Country.IsoCode, nil
}

func genPostfixResponse(destination string) string {
	return fmt.Sprintf("200 relay:[%s]\n", destination)
}

func getResult(email string) string {
	rand.Seed(time.Now().UnixNano())
	destination := destinationMap[defaultTarget][rand.Intn(len(defaultTarget))]

	domain, domainErr := getEmailDomain(email)
	if domainErr != nil {
		return destination
	}

	mxs, mxErr := getMx(domain)
	if mxErr != nil {
		return destination
	}

	for _, mx := range mxs {
		ip, ipErr := getIp(mx)
		if ipErr != nil {
			continue
		}

		country, countryErr := getCountryByIp(ip)
		if countryErr != nil {
			continue
		}

		log.Infof("Got country code: %s for domain:%s", country, domain)
		if value, ok := destinationMap[country]; ok {
			destination = value[rand.Intn(len(value))]
		}
		break
	}

	return destination
}
