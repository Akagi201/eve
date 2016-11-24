package main

import (
	"os"
	"os/signal"
	"regexp"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/jessevdk/go-flags"
	"github.com/mattermost/platform/model"
)

var opts struct {
	Config         string `long:"config" default:"conf/eve.ini" no-ini:"true" description:"config file"`
	UserName       string `long:"user_name" ini-name:"user_name" default:"eve" description:"user name"`
	UserFirst      string `long:"user_first" ini-name:"user_first" default:"Eve" description:"user first name"`
	UserLast       string `long:"user_last" ini-name:"user_last" default:"Bot" description:"user last name"`
	UserEmail      string `long:"user_email" ini-name:"user_email" default:"akagi201@gmail.com" description:"user email"`
	UserPasswd     string `long:"user_passwd" ini-name:"user_passwd" default:"" description:"user password"`
	TeamName       string `long:"team_name" ini-name:"team_name" default:"upmedia" description:"team name"`
	MatterMostURL  string `long:"mm_url" ini-name:"mm_url" default:"http://localhost:8065" description:"mattermost http service url"`
	ChannelLogName string `long:"channel_log" ini-name:"channel_log" default:"eve" description:"channel log name"`
}

const (
	robotName = "EVE"
)

var (
	log              *logrus.Logger
	client           *model.Client
	webSocketClient  *model.WebSocketClient
	debuggingChannel *model.Channel
	botUser          *model.User
	botTeam          *model.Team
	initialLoad      *model.InitialLoad
)

func init() {
	log = logrus.New()
	log.Level = logrus.InfoLevel
	f := new(logrus.TextFormatter)
	f.TimestampFormat = "2006-01-02 15:04:05"
	f.FullTimestamp = true
	log.Formatter = f
}

func setupGracefulShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			if webSocketClient != nil {
				webSocketClient.Close()
			}

			sendMsgToDebuggingChannel("_"+robotName+" has **stopped** running_", "")
			os.Exit(0)
		}
	}()
}

func printError(err *model.AppError) {
	println("\tError Details:")
	println("\t\t" + err.Message)
	println("\t\t" + err.Id)
	println("\t\t" + err.DetailedError)
}

func sendMsgToDebuggingChannel(msg string, replyToId string) {
	post := &model.Post{}
	post.ChannelId = debuggingChannel.Id
	post.Message = msg

	post.RootId = replyToId

	if _, err := client.CreatePost(post); err != nil {
		println("We failed to send a message to the logging channel")
		printError(err)
	}
}

func makeSureServerIsRunning() {
	if props, err := client.GetPing(); err != nil {
		println("There was a problem pinging the Mattermost server.  Are you sure it's running?")
		printError(err)
		os.Exit(1)
	} else {
		println("Server detected and is running version " + props["version"])
	}
}

func loginAsTheBotUser() {
	if loginResult, err := client.Login(opts.UserEmail, opts.UserPasswd); err != nil {
		println("There was a problem logging into the Mattermost server.  Are you sure ran the setup steps from the README.md?")
		printError(err)
		os.Exit(1)
	} else {
		botUser = loginResult.Data.(*model.User)
	}
}

func updateTheBotUserIfNeeded() {
	if botUser.FirstName != opts.UserFirst || botUser.LastName != opts.UserLast || botUser.Username != opts.UserName {
		botUser.FirstName = opts.UserFirst
		botUser.LastName = opts.UserLast
		botUser.Username = opts.UserName

		if updateUserResult, err := client.UpdateUser(botUser); err != nil {
			println("We failed to update the Sample Bot user")
			printError(err)
			os.Exit(1)
		} else {
			botUser = updateUserResult.Data.(*model.User)
			println("Looks like this might be the first run so we've updated the bots account settings")
		}
	}
}

func initialLoads() {
	if initialLoadResults, err := client.GetInitialLoad(); err != nil {
		println("We failed to get the initial load")
		printError(err)
		os.Exit(1)
	} else {
		initialLoad = initialLoadResults.Data.(*model.InitialLoad)
	}
}

func findBotTeam() {
	for _, team := range initialLoad.Teams {
		if team.Name == opts.TeamName {
			botTeam = team
			break
		}
	}

	if botTeam == nil {
		println("We do not appear to be a member of the team '" + opts.TeamName + "'")
		os.Exit(1)
	}
}

func createBotDebuggingChannelIfNeeded() {
	if channelsResult, err := client.GetChannels(""); err != nil {
		println("We failed to get the channels")
		printError(err)
	} else {
		channelList := channelsResult.Data.(*model.ChannelList)
		for _, channel := range *channelList {

			// The logging channel has alredy been created, lets just use it
			if channel.Name == opts.ChannelLogName {
				debuggingChannel = channel
				return
			}
		}
	}

	// Looks like we need to create the logging channel
	channel := &model.Channel{}
	channel.Name = opts.ChannelLogName
	channel.DisplayName = "Debugging For Sample Bot"
	channel.Purpose = "This is used as a test channel for logging bot debug messages"
	channel.Type = model.CHANNEL_OPEN
	if channelResult, err := client.CreateChannel(channel); err != nil {
		println("We failed to create the channel " + opts.ChannelLogName)
		printError(err)
	} else {
		debuggingChannel = channelResult.Data.(*model.Channel)
		println("Looks like this might be the first run so we've created the channel " + opts.ChannelLogName)
	}
}

func handleMsgFromDebuggingChannel(event *model.WebSocketEvent) {
	// If this isn't the debugging channel then lets ingore it
	if event.Broadcast.ChannelId != debuggingChannel.Id {
		return
	}

	// Lets only reponded to messaged posted events
	if event.Event != model.WEBSOCKET_EVENT_POSTED {
		return
	}

	println("responding to debugging channel msg")

	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {

		// ignore my events
		if post.UserId == botUser.Id {
			return
		}

		// if you see any word matching 'alive' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)alive(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'up' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)up(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'running' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)running(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'hello' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)hello(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}
	}

	sendMsgToDebuggingChannel("I did not understand you!", post.Id)
}

func handleWebSocketResponse(event *model.WebSocketEvent) {
	handleMsgFromDebuggingChannel(event)
}

func main() {
	p := flags.NewParser(&opts, flags.Default)

	_, err := p.Parse()
	if err != nil {
		if !strings.Contains(err.Error(), "Usage") {
			log.Fatalf("Parse cli failed, error: %v", err)
		} else {
			return
		}
	}

	if opts.Config != "" {
		iniParser := flags.NewIniParser(p)
		iniParser.ParseAsDefaults = true
		err := iniParser.ParseFile(opts.Config)
		if err != nil {
			log.Fatalf("Parse ini file failed, error: %v", err)
		}
	}

	setupGracefulShutdown()

	client = model.NewClient(opts.MatterMostURL)

	// Lets test to see if the mattermost server is up and running
	makeSureServerIsRunning()

	// lets attempt to login to the Mattermost server as the bot user
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	loginAsTheBotUser()

	// If the bot user doesn't have the correct information lets update his profile
	updateTheBotUserIfNeeded()

	// Lets load all the stuff we might need
	initialLoads()

	// Lets find our bot team
	findBotTeam()

	// This is an important step.  Lets make sure we use the botTeam
	// for all future web service requests that require a team.
	client.SetTeamId(botTeam.Id)

	// Lets create a bot channel for logging debug messages into
	createBotDebuggingChannelIfNeeded()
	sendMsgToDebuggingChannel("_"+robotName+" has **started** running_", "")

	// Lets start listening to some channels via the websocket!
	webSocketClient, appErr := model.NewWebSocketClient("ws://localhost:8065", client.AuthToken)
	if appErr != nil {
		println("We failed to connect to the web socket")
		printError(appErr)
	}

	webSocketClient.Listen()

	go func() {
		for {
			select {
			case resp := <-webSocketClient.EventChannel:
				handleWebSocketResponse(resp)
			}
		}
	}()

	// You can block forever with
	select {}
}
