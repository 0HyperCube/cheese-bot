package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/bwmarrin/discordgo"
)

type User struct {
	PersonalAccount string
	Organisations   []string
}

type Account struct {
	Balance  int
	Personal bool
}

type Data struct {
	Users    map[string]*User
	Accounts map[string]*Account
}

// Variables used for command line parameters
var (
	Token string
)

var (
	data Data
	err  error
)

func read_data() {
	raw_data, err := ioutil.ReadFile("data.json")
	if err != nil {
		log.Fatal(err)
	}
	err = json.Unmarshal(raw_data, &data)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("data", data)
}

func save_data() {
	serialised, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("saving", string(serialised), data)
	err = ioutil.WriteFile("data.json", serialised, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.Parse()

	read_data()
	// data.Users = make(map[string]User)
	// data.Accounts = make(map[string]Account)
	// data.Users["090"] = User{PersonalAccount: "James", Organisations: []string{"bank"}}
	// data.Accounts["James"] = Account{Balance: 10}
	// data.Accounts["bank"] = Account{Balance: 10}
	// fmt.Println("data", data)
	// save_data()
}

func format_cheesecoins(cheesecoins int) string {
	return fmt.Sprintf("%.2f", float32(cheesecoins)/100)
}

func check_new_user(user *discordgo.User) {
	if _, isMapContainsKey := data.Users[user.ID]; !isMapContainsKey {
		data.Accounts[user.Username] = &Account{Balance: 0, Personal: true}
		data.Users[user.ID] = &User{PersonalAccount: user.Username, Organisations: []string{}}
	}
}

func add_commands(session *discordgo.Session) {
	org_acounds_num := 0
	for _, account := range data.Accounts {
		if !account.Personal {
			org_acounds_num += 1
		}
	}

	org_account_choices := make([]*discordgo.ApplicationCommandOptionChoice, org_acounds_num)
	all_account_choices := make([]*discordgo.ApplicationCommandOptionChoice, len(data.Accounts))
	all_index := 0
	org_index := 0
	fmt.Println(data, all_account_choices)
	for name, account := range data.Accounts {
		fmt.Println("account", name, all_index)

		formatted_name := name
		if account.Personal {
			formatted_name += " (Personal)"
		} else {
			formatted_name += " (organisation)"
			org_account_choices[org_index] = &discordgo.ApplicationCommandOptionChoice{Name: formatted_name, Value: name}
			org_index++
		}
		all_account_choices[all_index] = &discordgo.ApplicationCommandOptionChoice{Name: formatted_name, Value: name}
		all_index++
	}
	fmt.Println(all_account_choices[0].Name)
	command := []*discordgo.ApplicationCommand{
		{
			Name:        "help",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Description of the bot's commands.",
		}, {
			Name:        "balances",
			Type:        discordgo.ChatApplicationCommand,
			Description: "All of your balances.",
		}, {
			Name:        "pay",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Give someone cheesecoins.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "recipiant",
					Description: "Recipiant of the payment",
					Required:    true,
					Choices:     all_account_choices,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "cheesecoin",
					Description: "Amount of cheesecoins",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "from_org",
					Description: "The account organisation the cheesecoins from (must be owned by you). Default is personal",
					Required:    false,
					Choices:     org_account_choices,
				},
			},
		},
	}

	_, err = session.ApplicationCommandBulkOverwrite(session.State.User.ID, "", command)

	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	// Create a new Discord session using the provided bot token.
	session, err := discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	session.AddHandler(messageCreate)

	// Just like the ping pong example, we only care about receiving message
	// events in this example.
	session.Identify.Intents = discordgo.IntentsDirectMessages

	// Open a websocket connection to Discord and begin listening.
	err = session.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	fmt.Println(err)

	add_commands(session)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running. Press CTRL-C to exit.")

	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	<-stop
	fmt.Println("Closing connection")
	save_data()
	// Cleanly close down the Discord session.
	session.Close()

}

func help(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Author:      &discordgo.MessageEmbedAuthor{},
		Color:       0xFFE41E,
		Description: "Cheese bot commands",
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "/help",
				Value:  "Displays help about all commands (this message)",
				Inline: true,
			},
			{
				Name:   "/balances",
				Value:  "View your personal balance and the balance of your organization(s)",
				Inline: true,
			},
			{
				Name:   "/pay [recipiant] [cheesecoins] +optional (from [account])",
				Value:  "Pays [recipiant] [cheesecoins] from an account (default is personal account)",
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:     "Help",
	}

	session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{embed},
	}})
}
func format_account(name string) string {
	account := data.Accounts[name]
	return fmt.Sprintf("**%s**: %scc\n", name, format_cheesecoins(account.Balance))
}
func balances(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate) {

	user := interaction.User
	check_new_user(user)
	user_data := data.Users[user.ID]
	description := format_account(user_data.PersonalAccount)
	for _, account_name := range user_data.Organisations {
		description += format_account(account_name)
	}

	embed := &discordgo.MessageEmbed{
		Author:      &discordgo.MessageEmbedAuthor{},
		Color:       0xFFE41E,
		Description: description,

		Timestamp: time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:     "Balances",
	}

	session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{embed},
	}})
}

func user_has_org(user *discordgo.User, org string) bool {
	arr := data.Users[user.ID].Organisations
	for _, i := range arr {
		if i == org {
			return true
		}
	}
	return false
}

func pay_result(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) string {
	user := interaction.User
	account := interaction_data.Options[0].StringValue()
	account_name := interaction_data.Options[0].Name
	amount := int(interaction_data.Options[1].IntValue())
	payer := data.Users[user.ID].PersonalAccount
	payer_name := payer + " (Personal)"
	if len(interaction_data.Options) > 2 {
		payer = interaction_data.Options[2].StringValue()
		payer_name = interaction_data.Options[2].Name
		if !user_has_org(user, payer) {
			return fmt.Sprint("**ERROR:** You do not own the ", interaction_data.Options[2].Name)
		}
	}
	if amount < 0 {
		return fmt.Sprint("**ERROR:** Cannot pay negative cheesecoins")
	}
	if data.Accounts[payer].Balance < amount {
		return fmt.Sprint("**ERROR:** ", payer_name, " has only ", format_cheesecoins(data.Accounts[payer].Balance))
	}
	data.Accounts[payer].Balance -= amount
	data.Accounts[account].Balance += amount
	return fmt.Sprint("Sucsessfully transfered ", format_cheesecoins(amount), " from ", account_name, " to ", payer_name)
}

func pay(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) {
	embed := &discordgo.MessageEmbed{
		Author:      &discordgo.MessageEmbedAuthor{},
		Color:       0xFFE41E,
		Description: pay_result(session, channel, interaction, interaction_data),

		Timestamp: time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:     "Payment",
	}

	session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{embed},
	}})
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	interaction_data := interaction.ApplicationCommandData()

	// Ignore all messages created by the bot itself
	// This isn't required in this specific example but it's a good practice.
	if interaction_data.TargetID == session.State.User.ID {
		return
	}

	channel, err := session.State.Channel(interaction.ChannelID)
	if err != nil {
		if channel, err = session.Channel(interaction.ChannelID); err != nil {
			return
		}
	}

	if channel.Type != discordgo.ChannelTypeDM {
		return
	}

	fmt.Println("interaction", interaction_data.Name, "interaction", interaction)

	// Handle help
	if interaction_data.Name == "help" {
		help(session, channel, interaction)
	} else if interaction_data.Name == "balances" {
		balances(session, channel, interaction)
	} else if interaction_data.Name == "pay" {
		pay(session, channel, interaction, interaction_data)
	}
}
