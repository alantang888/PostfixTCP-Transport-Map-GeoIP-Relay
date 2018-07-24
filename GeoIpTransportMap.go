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
	cli "gopkg.in/urfave/cli.v1"
	"io"
	"log"
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
}

func main() {
	app := argsParserSetup()

	// Args handling setup
	app.Action = argsHandler

	// Args parse
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}

	// TODO: handle geoip db update
	listenInterface := "0.0.0.0"
	listenPort := "2527"

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%s", listenInterface, listenPort))
	if err != nil {
		log.Fatal(fmt.Sprintf("Listen %s:%s error: %s\n", listenInterface, listenPort, err.Error()))
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			// TODO: Change to logger
			fmt.Printf("Connection accept error.\n")
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
	// TODO: Change to logger
	fmt.Printf("Connect from '%v'.\n", conn.RemoteAddr())
	reader := bufio.NewReader(conn)
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// TODO: Change to logger
				fmt.Printf("Connection closed from %v.\n", conn.RemoteAddr())
			} else {
				// TODO: Change to logger
				fmt.Printf("Read from %v error: '%s'.\n", conn.RemoteAddr(), err.Error())
			}
			conn.Close()
			return
		}
		length := len(data)
		//if length < 1{
		dataString := string(data[:length-1])

		// TODO: Change to logger
		fmt.Printf("Received '%s'\n", dataString)

		result := getResult(dataString)
		conn.Write([]byte(result))
	}
}

func getEmailDomain(email string) (string, error) {
	splitedEmail := strings.Split(email, "@")
	if len(splitedEmail) != 2 {
		return "", errors.New(fmt.Sprintf("Email address invalid: %v", email))
	}

	// TODO: Add info log
	return splitedEmail[1], nil
}

func getMx(domain string) ([]*net.MX, error) {
	// LookupMX will return a MX list sorted by priority. So no need to sort
	mxs, err := net.LookupMX(domain)

	if err != nil {
		// TODO: Change to logger
		fmt.Printf("Get MX error on %v: %v\n", domain, err)
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
		// TODO: Change to logger
		fmt.Printf("Get IP error on %v: %v\n", mx.Host, err)
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

	return net.IP{}, errors.New(fmt.Sprint("Get IP error from MX record(s)."))
}

func getCountryByIp(ipAddress net.IP) (string, error) {
	// TODO: reduce read file. should read from cache by geoip2.FromBytes()
	db, err := geoip2.Open("GeoLite2-Country.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	record, err := db.Country(ipAddress)
	if err != nil {
		// TODO: Change to logger
		fmt.Printf("Get country error on %v: %v\n", ipAddress.String(), err)
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
		// TODO: Change to logger
		fmt.Printf("%s\n", domainErr.Error())
		return genPostfixResponse(destination)
	}

	mxs, mxErr := getMx(domain)
	if mxErr != nil {
		return genPostfixResponse(destination)
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

		// TODO: Change to logger
		fmt.Printf("Got country code: %s for domain:%s\n", country, domain)
		if value, ok := destinationMap[country]; ok {
			destination = value[rand.Intn(len(value))]
		}
		break
	}

	return genPostfixResponse(destination)
}
