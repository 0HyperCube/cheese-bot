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
	SuperUser       bool
	Organisations   []string
}

type Account struct {
	Name    string
	Balance int
}

type Data struct {
	Users                map[string]*User
	PersonalAccounts     map[string]*Account
	OrganisationAccounts map[string]*Account
	NextPersonal         int
	NextOrg              int
	TaxRate              float64
}

// Variables used for command line parameters
var (
	Token string
)

var (
	data     Data
	treasury *Account
	err      error
)

// Read the json file - called on init
func read_data() {
	// Read the file
	raw_data, err := ioutil.ReadFile("data.json")
	if err != nil {
		log.Fatal(err)
	}
	// Unmarshal - convert to a Go struct
	err = json.Unmarshal(raw_data, &data)
	if err != nil {
		log.Fatal(err)
	}

	// Assign treasury
	treasury = data.OrganisationAccounts["1000"]
}

// Save the json file - called on shutdown
func save_data() {
	// Marshal / serialise - convert the struct into an array of bytes representing a json string
	serialised, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err)
	}
	// Write the file back to the disk
	err = ioutil.WriteFile("data.json", serialised, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

// Called as the first function to run from this module
func init() {
	// Parse the bot token as a command line arg from the format `go run . -t [token]`
	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.Parse()

	// Read the json file
	read_data()
}

// Utility for finding an account that could be a user or an organisation account.
// Returns the account and a bool for sucsess
func get_account(id string) (*Account, bool) {
	val, ok := data.PersonalAccounts[id]
	if !ok {
		val, ok = data.OrganisationAccounts[id]
	}
	return val, ok
}

// Format cheesecoins from an int to the decimal format string
func format_cheesecoins(cheesecoins int) string {
	return fmt.Sprintf("%.2fcc", float32(cheesecoins)/100)
}

// If a user with this id has not been saved then create a new user and a new personal account.
// Should be called before searching for the user data.
func check_new_user(user *discordgo.User) {
	if _, isMapContainsKey := data.Users[user.ID]; !isMapContainsKey {
		data.PersonalAccounts[fmt.Sprint(data.NextPersonal)] = &Account{Name: user.Username, Balance: 0}
		data.Users[user.ID] = &User{PersonalAccount: fmt.Sprint(data.NextPersonal), Organisations: []string{}}
		data.NextPersonal += 1
	}
}

// Bulk overrides the bot's slash commands and adds new ones.
func add_commands(session *discordgo.Session) {
	user_account_choices := make([]*discordgo.ApplicationCommandOptionChoice, len(data.PersonalAccounts))
	index := 0
	for id, account := range data.PersonalAccounts {
		user_account_choices[index] = &discordgo.ApplicationCommandOptionChoice{Name: account.Name + " (Personal)", Value: id}
		index++
	}

	user_choices := make([]*discordgo.ApplicationCommandOptionChoice, len(data.PersonalAccounts))
	index = 0
	for id, user := range data.Users {
		user_choices[index] = &discordgo.ApplicationCommandOptionChoice{Name: data.PersonalAccounts[user.PersonalAccount].Name + " (Person)", Value: id}
		index++
	}

	org_account_choices := make([]*discordgo.ApplicationCommandOptionChoice, len(data.OrganisationAccounts))
	index = 0
	for id, account := range data.OrganisationAccounts {
		org_account_choices[index] = &discordgo.ApplicationCommandOptionChoice{Name: account.Name + " (Organisation)", Value: id}
		index++
	}

	all_account_choices := append(user_account_choices[:], org_account_choices[:]...)

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
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
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
		}, {
			Name:        "transfer_org",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Transfer an organisation you own to another user.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "organisation",
					Description: "The organisation you wish to transfer (must be owned by you).",
					Required:    true,
					Choices:     org_account_choices,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "new_owner",
					Description: "The new owner of the organisation",
					Required:    true,
					Choices:     user_choices,
				},
			},
		}, {
			Name:        "rename_org",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Rename an organisation you own.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "organisation",
					Description: "The organisation you wish to rename (must be owned by you).",
					Required:    true,
					Choices:     org_account_choices,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "new_name",
					Description: "The new name of the organisation",
					Required:    true,
				},
			},
		}, {
			Name:        "sudo_set_tax",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Set the tax rate.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
					Name:        "new_tax",
					Description: "The new tax rate (0% to 100%).",
					Required:    true,
				},
			},
		},
	}

	_, err := session.ApplicationCommandBulkOverwrite(session.State.User.ID, "", command)

	if err != nil {
		log.Fatal(err)
	}
}

// Utility function to create an embed in response to an interaction
func create_embed(name string, session *discordgo.Session, interaction *discordgo.InteractionCreate, description string, Fields []*discordgo.MessageEmbedField) {
	embed := &discordgo.MessageEmbed{
		Author:      &discordgo.MessageEmbedAuthor{},
		Color:       0xFFE41E,
		Description: description,

		Timestamp: time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:     name,
		Fields:    Fields,
	}

	// Send the embed as a response to the provided interaction
	session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{embed},
	}})
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

	// Initalises the slash commands
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

// Displays the help embed as a response to the provided interaction
func help(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate) {
	create_embed("Help", session, interaction, "**List of cheese bot commands**", []*discordgo.MessageEmbedField{
		{
			Name:   "/help",
			Value:  "Displays help about all commands (this message)",
			Inline: false,
		},
		{
			Name:   "/balances",
			Value:  "View your personal balance and the balance of your organization(s)",
			Inline: false,
		},
		{
			Name:   "/pay",
			Value:  "Pays [recipiant] [cheesecoins] from an account (default is personal account)",
			Inline: false,
		},
		{
			Name:   "/transfer_org",
			Value:  "Transfers [organisation] to [new_owner].",
			Inline: false,
		},
		{
			Name:   "/rename_org",
			Value:  "Renames [organisation] to [new_name].",
			Inline: false,
		},
		{
			Name:   "/sudo_set_tax",
			Value:  "Sets the tax rate to [new_tax]%. Can only be done by super user (i.e. head of bank).",
			Inline: false,
		},
	})
}

// Utility function for providing a string of an account
func format_account(account *Account) string {
	return fmt.Sprintf("%-20s %s\n", account.Name+":", format_cheesecoins(account.Balance))
}

// Utility function to find the total currency
func total_currency() int {
	total := 0
	for _, a := range data.PersonalAccounts {
		total += a.Balance
	}
	for _, a := range data.OrganisationAccounts {
		total += a.Balance
	}
	return total
}

// Displays the balance embed as a response to the provided interaction
func balances(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate) {
	user := interaction.User
	check_new_user(user)

	// Get the user data from their discord id
	user_data := data.Users[user.ID]

	description := fmt.Sprintf("**Currency information**\n```\n%-20s %.2f%%\n%-20s %s\n```\n**Your accounts**\n```", "Tax Rate:", data.TaxRate, "Total Currency:", format_cheesecoins(total_currency()))

	// Add their personal account to the resulting string
	description += format_account(data.PersonalAccounts[user_data.PersonalAccount])

	// Add their organisations to the resulting string
	for _, account_name := range user_data.Organisations {
		description += format_account(data.OrganisationAccounts[account_name])
	}

	description += "```"

	create_embed("Balances", session, interaction, description, []*discordgo.MessageEmbedField{})
}

// Untility function to check if the user has control of the organisation specified.
// Go does not have an array contains element function.
func user_has_org(user *discordgo.User, org string, delete_if_found bool) bool {
	for index, i := range data.Users[user.ID].Organisations {
		if i == org {
			if delete_if_found {
				data.Users[user.ID].Organisations = append(data.Users[user.ID].Organisations[:index], data.Users[user.ID].Organisations[index+1:]...)

			}
			return true
		}
	}
	return false
}

// Utility function to carry out a transaction and return the result as a string
func pay_result(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) string {
	user := interaction.User
	check_new_user(user)

	// Get the recipiant
	recipiant_account, _ := get_account(interaction_data.Options[0].StringValue())
	recipiant_name := recipiant_account.Name

	// Get the transaction amount
	float_amount, _ := interaction_data.Options[1].Value.(float64)
	amount := int(float_amount * 100)

	// Get the payer - the default being the current user's personal account
	payer := data.Users[user.ID].PersonalAccount
	payer_account, _ := get_account(payer)
	payer_name := payer_account.Name + " (Personal)"
	if len(interaction_data.Options) > 2 {
		payer = interaction_data.Options[2].StringValue()
		payer_account, _ = get_account(payer)
		payer_name = payer_account.Name
		if !user_has_org(user, payer, false) {
			return fmt.Sprint("**ERROR:** You do not own the ", payer_name, " organsiation")
		}
	}

	// Check for negatives
	if amount < 0 {
		return fmt.Sprint("**ERROR:** Cannot pay negative cheesecoins")
	}

	// Check for paying too much
	if payer_account.Balance < amount {
		return fmt.Sprint("**ERROR:** ", payer_name, " has only ", format_cheesecoins(payer_account.Balance))
	}

	// Calculate tax
	tax := int(float64(amount) * data.TaxRate / 100)

	payer_account.Balance -= amount
	recipiant_account.Balance += amount - tax
	treasury.Balance += tax
	return fmt.Sprint("Sucsessfully transfered ", format_cheesecoins(amount), " from ", payer_name, " to ", recipiant_name,
		".\n```\nAmount Payed    ", format_cheesecoins(amount), "\nTax           - ", format_cheesecoins(tax), "\nRecieved      = ", format_cheesecoins(amount-tax), "\n```")
}

// Carries out a payment and displays the pay embed as a response to the provided interaction
func pay(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) {
	create_embed("Payment", session, interaction, pay_result(session, channel, interaction, interaction_data), []*discordgo.MessageEmbedField{})
}

// Tries to transfer an organisation and returns the string result
func transfer_organisation_result(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) string {
	user := interaction.User
	check_new_user(user)

	// Get the organisation
	organsiation := interaction_data.Options[0].StringValue()
	organisation_name := data.OrganisationAccounts[organsiation].Name

	if !user_has_org(user, organsiation, true) {
		return fmt.Sprint("**ERROR:** You do not own ", organisation_name)
	}

	// Get the recipiant
	recipiant := data.Users[interaction_data.Options[1].StringValue()]
	recipiant_name := data.PersonalAccounts[recipiant.PersonalAccount].Name

	recipiant.Organisations = append(recipiant.Organisations, organsiation)

	return fmt.Sprint(
		"Sucessfully transfered ", organisation_name, " to ", recipiant_name)
}

// Tries to transfer an organisation and displays the transfer organisation embed as a response to the provided interaction
func transfer_organisation(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) {
	create_embed("Transfer organisation", session, interaction, transfer_organisation_result(session, channel, interaction, interaction_data), []*discordgo.MessageEmbedField{})
}

// Tries to rename an organisation and returns the string result
func rename_organisation_result(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) string {
	user := interaction.User
	check_new_user(user)

	// Get the organisation
	organsiation := interaction_data.Options[0].StringValue()
	organisation_account := data.OrganisationAccounts[organsiation]
	organisation_name := organisation_account.Name

	if !user_has_org(user, organsiation, false) {
		return fmt.Sprint("**ERROR:** You do not own ", organisation_name)
	}

	new_name := interaction_data.Options[1].StringValue()

	organisation_account.Name = new_name

	return fmt.Sprint(
		"Sucessfully renamed ", organisation_name, " to ", new_name)
}

// Tries to rename an organisation and displays the rename organisation embed as a response to the provided interaction
func rename_organisation(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) {
	create_embed("Rename organisation", session, interaction, rename_organisation_result(session, channel, interaction, interaction_data), []*discordgo.MessageEmbedField{})
}

// Tries to rename an organisation and returns the string result
func set_tax_result(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) string {
	user := interaction.User
	check_new_user(user)

	if !data.Users[user.ID].SuperUser {
		return fmt.Sprint("**ERROR:** You are not a super user")
	}

	data.TaxRate = interaction_data.Options[0].Value.(float64)

	return fmt.Sprint("Sucessfully set tax to ", data.TaxRate, "%.")
}

// Tries to set tax and displays the set tax embed as a response to the provided interaction
func set_tax(session *discordgo.Session, channel *discordgo.Channel, interaction *discordgo.InteractionCreate, interaction_data discordgo.ApplicationCommandInteractionData) {
	create_embed("Set Tax", session, interaction, set_tax_result(session, channel, interaction, interaction_data), []*discordgo.MessageEmbedField{})
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	interaction_data := interaction.ApplicationCommandData()

	user := interaction.User

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

	fmt.Println("interaction", interaction_data.Name, "interaction", interaction, "From ", user.Username)

	// Handle interactions
	if interaction_data.Name == "help" {
		help(session, channel, interaction)
	} else if interaction_data.Name == "balances" {
		balances(session, channel, interaction)
	} else if interaction_data.Name == "pay" {
		pay(session, channel, interaction, interaction_data)
	} else if interaction_data.Name == "transfer_org" {
		transfer_organisation(session, channel, interaction, interaction_data)
	} else if interaction_data.Name == "rename_org" {
		rename_organisation(session, channel, interaction, interaction_data)
	} else if interaction_data.Name == "sudo_set_tax" {
		set_tax(session, channel, interaction, interaction_data)
	}
}
