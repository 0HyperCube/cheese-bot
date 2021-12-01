package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"os/signal"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sahilm/fuzzy"
)

type User struct {
	PersonalAccount string
	SuperUser       bool
	Mp              bool
	LastPay         time.Time
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
	TransactionTax       float64
	WealthTax            float64
	MpPay                int
	LastWealthTax        time.Time
}

const (
	AutoCompleteNonSelfUsers int8 = iota
	AutoCompleteAllAccounts
	AutoCompleteOwnedOrgs
	AutoCompleteNone
)

// Variables used for command line parameters
var (
	Token string
)

type HandlerData struct {
	session          *discordgo.Session
	channel          *discordgo.Channel
	interaction      *discordgo.InteractionCreate
	interaction_data discordgo.ApplicationCommandInteractionData
	user             *discordgo.User
}

var (
	data     Data
	treasury *Account

	commandHandlers = map[string]func(data_handler HandlerData){
		"help": func(data_handler HandlerData) {
			create_embed("Help", data_handler.session, data_handler.interaction, "**List of cheese bot commands**", []*discordgo.MessageEmbedField{
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
					Name:   "/delete_org",
					Value:  "Deletes [organisation] and transfers the remaining funds to your personal account.",
					Inline: false,
				},
				{
					Name:   "/create_org",
					Value:  "Creates a new organisation with [name] and gives you ownership.",
					Inline: false,
				},
				{
					Name:   "/mp_daily_rollcall",
					Value:  "Claim your daily 2cc if you are an MP.",
					Inline: false,
				},
				{
					Name:   "/sudo_set_wealth_tax",
					Value:  "Sets the wealth tax rate to [new_tax]%. Can only be done by super user (i.e. head of bank).",
					Inline: false,
				},
				{
					Name:   "/sudo_set_transaction_tax",
					Value:  "Sets the transaction tax rate to [new_tax]%. Can only be done by super user (i.e. head of bank).",
					Inline: false,
				},
			})
		},
		"balances": func(data_handler HandlerData) {
			// Get the user data from their discord id
			user_data := data.Users[data_handler.user.ID]

			description := fmt.Sprintf("**Currency information**\n```\n%-20s %.2f%%\n%-20s %.2f%%\n%-20s %s\n```\n**Your accounts**\n```",
				"Wealth Tax:", data.WealthTax, "Transaction Tax:", data.TransactionTax, "Total Currency:", format_cheesecoins(total_currency()))

			// Add their personal account to the resulting string
			description += format_account(data.PersonalAccounts[user_data.PersonalAccount])

			// Add their organisations to the resulting string
			for _, account_name := range user_data.Organisations {
				description += format_account(data.OrganisationAccounts[account_name])
			}

			description += "```"

			create_embed("Balances", data_handler.session, data_handler.interaction, description, []*discordgo.MessageEmbedField{})
		},
		"pay": func(data_handler HandlerData) {
			// Get the recipiant
			recipiant_account, _ := get_account(data_handler.interaction_data.Options[0].StringValue())
			recipiant_name := recipiant_account.Name

			// Get the transaction amount
			float_amount, _ := data_handler.interaction_data.Options[1].Value.(float64)
			amount := int(float_amount * 100)

			// Get the payer - the default being the current user's personal account
			payer := data.Users[data_handler.user.ID].PersonalAccount
			payer_account, _ := get_account(payer)
			payer_name := payer_account.Name + " (Personal)"
			if len(data_handler.interaction_data.Options) > 2 {
				payer = data_handler.interaction_data.Options[2].StringValue()
				payer_account, _ = get_account(payer)
				payer_name = payer_account.Name
				if !user_has_org(data_handler.user, payer, false) {
					create_embed("Payment", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own the ", payer_name, " organsiation"), []*discordgo.MessageEmbedField{})
					return
				}
			}

			sucsess, err, tax := transaction(amount, payer_account, recipiant_account, payer_name, data_handler.session, nil)
			if !sucsess {
				create_embed("Payment", data_handler.session, data_handler.interaction, err, []*discordgo.MessageEmbedField{})
				return
			}

			create_embed("Payment", data_handler.session, data_handler.interaction, fmt.Sprint("Sucsessfully transfered ", format_cheesecoins(amount), " from ", payer_name, " to ", recipiant_name,
				".\n```\nAmount Payed    ", format_cheesecoins(amount), "\nTax           - ", format_cheesecoins(tax), "\nRecieved      = ", format_cheesecoins(amount-tax), "\n```"),
				[]*discordgo.MessageEmbedField{})
		},
		"transfer_org": func(data_handler HandlerData) {
			// Get the organisation
			organsiation := data_handler.interaction_data.Options[0].StringValue()
			organisation_name := data.OrganisationAccounts[organsiation].Name

			if !user_has_org(data_handler.user, organsiation, true) {
				create_embed("Transfer organisation", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own ", organisation_name), []*discordgo.MessageEmbedField{})
				return
			}

			// Get the recipiant
			recipiant := data.Users[data_handler.interaction_data.Options[1].StringValue()]
			recipiant_name := data.PersonalAccounts[recipiant.PersonalAccount].Name

			recipiant.Organisations = append(recipiant.Organisations, organsiation)

			create_embed("Transfer organisation", data_handler.session, data_handler.interaction, fmt.Sprint(
				"Sucessfully transfered ", organisation_name, " to ", recipiant_name), []*discordgo.MessageEmbedField{})
		},
		"create_org": func(data_handler HandlerData) {
			name := data_handler.interaction_data.Options[0].StringValue()

			data.Users[data_handler.user.ID].Organisations = append(data.Users[data_handler.user.ID].Organisations, fmt.Sprint(data.NextOrg))
			data.OrganisationAccounts[fmt.Sprint(data.NextOrg)] = &Account{Name: name, Balance: 0}
			data.NextOrg += 1

			create_embed("Create organisation", data_handler.session, data_handler.interaction, fmt.Sprint(
				"Sucessfully created ", name, " which is owned by ", data.PersonalAccounts[data.Users[data_handler.user.ID].PersonalAccount].Name), []*discordgo.MessageEmbedField{})
		},
		"answer_mp_rollcall": func(data_handler HandlerData) {
			cheese_user := data.Users[data_handler.user.ID]

			if !cheese_user.Mp {
				create_embed("Rollcall",
					data_handler.session, data_handler.interaction, "You are not an MP. Only MPs can claim this benefit.", []*discordgo.MessageEmbedField{})
				return
			}
			duration := time.Since(cheese_user.LastPay)
			if duration.Hours() < 18 {
				create_embed("Rollcall", data_handler.session, data_handler.interaction, fmt.Sprint("You can claim this benefit only once per day. You have last claimed it ", duration.Round(time.Second).String(), " ago"),
					[]*discordgo.MessageEmbedField{})
				return
			}

			cheese_user.LastPay = time.Now()

			sucsess, err, _ := transaction(data.MpPay, treasury, data.PersonalAccounts[cheese_user.PersonalAccount], "Treasury", data_handler.session, data_handler.interaction)
			if !sucsess {
				create_embed("Rollcall", data_handler.session, data_handler.interaction, err, []*discordgo.MessageEmbedField{})
			}
		},
		"rename_org": func(data_handler HandlerData) {
			// Get the organisation
			organsiation := data_handler.interaction_data.Options[0].StringValue()
			organisation_account := data.OrganisationAccounts[organsiation]
			organisation_name := organisation_account.Name

			if !user_has_org(data_handler.user, organsiation, false) {
				create_embed("Rename organisation", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own ", organisation_name), []*discordgo.MessageEmbedField{})
				return
			}

			new_name := data_handler.interaction_data.Options[1].StringValue()

			organisation_account.Name = new_name

			create_embed("Rename organisation", data_handler.session, data_handler.interaction, fmt.Sprint(
				"Sucessfully renamed ", organisation_name, " to ", new_name), []*discordgo.MessageEmbedField{})
		},
		"delete_org": func(data_handler HandlerData) {
			user_account := data.PersonalAccounts[data.Users[data_handler.user.ID].PersonalAccount]

			// Get the organisation
			organsiation := data_handler.interaction_data.Options[0].StringValue()
			organisation_name := data.OrganisationAccounts[organsiation].Name

			if !user_has_org(data_handler.user, organsiation, true) {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own ", organisation_name), []*discordgo.MessageEmbedField{})
				return
			}

			org_account := data.OrganisationAccounts[organsiation]
			sucsess, err, tax := transaction(org_account.Balance, org_account, user_account, "destroyed organisation", data_handler.session, nil)
			if !sucsess {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, err, []*discordgo.MessageEmbedField{})
				return
			}

			delete(data.OrganisationAccounts, organsiation)

			create_embed("Delete organisation", data_handler.session, data_handler.interaction, fmt.Sprint(
				"Sucessfully deleted ", organisation_name, " all funds have been transfered to your personal account (with ", tax, " in tax)"), []*discordgo.MessageEmbedField{})
		},
		"sudo_set_wealth_tax": func(data_handler HandlerData) {
			if !data.Users[data_handler.user.ID].SuperUser {
				create_embed("Set Wealth Tax", data_handler.session, data_handler.interaction, "**ERROR:** You are not a super user", []*discordgo.MessageEmbedField{})
				return
			}

			data.WealthTax = data_handler.interaction_data.Options[0].Value.(float64)

			create_embed("Set Wealth Tax", data_handler.session, data_handler.interaction, fmt.Sprint("Sucessfully set wealth tax to ", data.WealthTax, "%."), []*discordgo.MessageEmbedField{})
		},
		"sudo_set_transaction_tax": func(data_handler HandlerData) {
			if !data.Users[data_handler.user.ID].SuperUser {
				create_embed("Set Transaction Tax", data_handler.session, data_handler.interaction, "**ERROR:** You are not a super user", []*discordgo.MessageEmbedField{})
				return
			}

			data.TransactionTax = data_handler.interaction_data.Options[0].Value.(float64)

			create_embed("Set Transaction Tax", data_handler.session, data_handler.interaction, fmt.Sprint("Sucessfully set transaction tax to ", data.TransactionTax, "%."), []*discordgo.MessageEmbedField{})
		},
	}
	commandAutocomplete = map[string][]int8{
		"help":                     {},
		"balances":                 {},
		"pay":                      {AutoCompleteAllAccounts, AutoCompleteNone, AutoCompleteOwnedOrgs},
		"transfer_org":             {AutoCompleteOwnedOrgs, AutoCompleteNonSelfUsers},
		"create_org":               {AutoCompleteNone},
		"rename_org":               {AutoCompleteOwnedOrgs, AutoCompleteNone},
		"answer_mp_rollcall":       {},
		"delete_org":               {AutoCompleteOwnedOrgs},
		"sudo_set_wealth_tax":      {AutoCompleteNone},
		"sudo_set_transaction_tax": {AutoCompleteNone},
	}
)

// Bulk overrides the bot's slash commands and adds new ones.
func add_commands(session *discordgo.Session) {
	all_account_choices := make([]*discordgo.ApplicationCommandOptionChoice, len(data.PersonalAccounts)+len(data.OrganisationAccounts))
	index := 0
	for id, account := range data.PersonalAccounts {
		all_account_choices[index] = &discordgo.ApplicationCommandOptionChoice{Name: account.Name + " (Personal)", Value: id}
		index++
	}
	for id, account := range data.OrganisationAccounts {
		all_account_choices[index] = &discordgo.ApplicationCommandOptionChoice{Name: account.Name + " (Organisation)", Value: id}
		index++
	}
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
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "recipiant",
					Description:  "Recipiant of the payment",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
					Name:        "cheesecoin",
					Description: "Amount of cheesecoins",
					Required:    true,
				},
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "from_org",
					Description:  "The account organisation the cheesecoins from (must be owned by you). Default is personal",
					Required:     false,
					Autocomplete: true,
				},
			},
		}, {
			Name:        "transfer_org",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Transfer an organisation you own to another user.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "organisation",
					Description:  "The organisation you wish to transfer (must be owned by you).",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "new_owner",
					Description:  "The new owner of the organisation",
					Required:     true,
					Autocomplete: true,
				},
			},
		}, {
			Name:        "rename_org",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Rename an organisation you own.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "organisation",
					Description:  "The organisation you wish to rename (must be owned by you).",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "new_name",
					Description: "The new name of the organisation",
					Required:    true,
				},
			},
		}, {
			Name:        "create_org",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Create an organisation.",
			Options: []*discordgo.ApplicationCommandOption{

				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "name",
					Description: "The name of the new organisation",
					Required:    true,
				},
			},
		}, {
			Name:        "delete_org",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Delete an organisation.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "organisation",
					Description:  "The organisation you wish to delete (must be owned by you).",
					Required:     true,
					Autocomplete: true,
				},
			},
		}, {
			Name:        "answer_mp_rollcall",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Show you are active and get payed for the day if you are an MP.",
		}, {
			Name:        "sudo_set_wealth_tax",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Set the wealth tax rate.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
					Name:        "new_tax",
					Description: "The new wealth tax rate (0% to 100%).",
					Required:    true,
				},
			},
		}, {
			Name:        "sudo_set_transaction_tax",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Set the transaction tax rate.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
					Name:        "new_tax",
					Description: "The new transaction tax rate (0% to 100%).",
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

	fmt.Print("Saving....")
}

func periodic_save() {
	for range time.Tick(time.Minute * 1) {
		save_data()
	}
}

// Applies welth tax to a specific account returning the log information for the user
func apply_wealth_tax_account(account *Account, name string) string {
	tax := int(math.Ceil(float64(account.Balance) * data.WealthTax / 100))
	account.Balance -= tax
	treasury.Balance += tax
	return fmt.Sprintf("\n%-20s %s", name+":", format_cheesecoins(tax))
}

// Applies wealth tax. Called every day
func apply_wealth_tax(session *discordgo.Session) {
	fmt.Print("Wealth tax.")
	for id, usr := range data.Users {
		result := apply_wealth_tax_account(data.PersonalAccounts[usr.PersonalAccount], "Personal")
		for _, org := range usr.Organisations {
			account := data.OrganisationAccounts[org]
			if account != treasury {
				result += apply_wealth_tax_account(account, data.OrganisationAccounts[org].Name)
			}
		}

		send_embed("Wealth Tax", session, id,
			fmt.Sprintf("Wealth tax has been applied at `%.2f%%`.\n\n**Payments**\n```%s\n```", data.WealthTax, result),
			[]*discordgo.MessageEmbedField{})

		fmt.Println(result) // Probably send this to the user to notify them of the tax
	}
}

func check_wealth_tax(session *discordgo.Session) {
	for range time.Tick(time.Minute * 1) {
		if time.Since(data.LastWealthTax).Hours() > 20 {
			data.LastWealthTax = data.LastWealthTax.Add(time.Hour * 24)
			apply_wealth_tax(session)
		}
	}
}

// Called as the first function to run from this module
func init() {
	r, _ := time.Now().MarshalJSON()

	// Parse the bot token as a command line arg from the format `go run . -t [token]`
	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.Parse()

	// Read the json file
	read_data()

	go periodic_save()

	fmt.Println(string(r), format_cheesecoins(total_currency()))
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

// Utility function to create an embed in response to an interaction
func send_embed(name string, session *discordgo.Session, user string, description string, Fields []*discordgo.MessageEmbedField) {
	embed := &discordgo.MessageEmbed{
		Author:      &discordgo.MessageEmbedAuthor{},
		Color:       0xFFE41E,
		Description: description,

		Timestamp: time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:     name,
		Fields:    Fields,
	}

	// Send the embed as a response to the provided interaction
	channel, err := session.UserChannelCreate(user)

	if err != nil {
		fmt.Println(err)
	} else {
		session.ChannelMessageSendEmbed(channel.ID, embed)
	}
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

func account_owner(account *Account) string {
	for id, usr := range data.Users {
		if data.PersonalAccounts[usr.PersonalAccount] == account {
			return id
		}
		for _, org := range usr.Organisations {
			if data.OrganisationAccounts[org] == account {
				return id
			}
		}
	}
	return ""
}

// Conducts a transaction. Returns `Sucsess bool`, `error string` and `tax int`
func transaction(amount int, payer_account *Account, recipiant_account *Account, payer_name string, session *discordgo.Session, interaction *discordgo.InteractionCreate) (bool, string, int) {
	// Check for negatives
	if amount < 0 {
		return false, "**ERROR:** Cannot pay negative cheesecoins", 0
	}

	// Check for paying too much
	if payer_account.Balance < amount {
		return false, fmt.Sprint("**ERROR:** ", payer_name, " has only ", format_cheesecoins(payer_account.Balance)), 0
	}

	// Calculate tax
	tax := int(math.Ceil(float64(amount) * data.TransactionTax / 100))

	payer_account.Balance -= amount
	recipiant_account.Balance += amount - tax
	treasury.Balance += tax

	if session != nil {
		recipiant_id := account_owner(recipiant_account)

		text := fmt.Sprint("You've recieved ", format_cheesecoins(amount), " from ", payer_name, " to ", recipiant_account.Name,
			".\n```\nAmount Payed    ", format_cheesecoins(amount), "\nTax           - ", format_cheesecoins(tax), "\nRecieved      = ", format_cheesecoins(amount-tax), "\n```")
		if interaction == nil {
			send_embed("Payment", session, recipiant_id,
				text,
				[]*discordgo.MessageEmbedField{})
		} else {
			create_embed("Payment", session, interaction,
				text,
				[]*discordgo.MessageEmbedField{})
		}
	}

	return true, "", tax
}

type option_choice []*discordgo.ApplicationCommandOptionChoice

func (x option_choice) String(i int) string {
	return x[i].Name
}

func (employ option_choice) Len() int {
	return len(employ)
}

// This function will be called (due to AddHandler above) every time a new
// interaction is created.
func interactionCreate(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	interaction_data := interaction.ApplicationCommandData()

	user := interaction.User
	check_new_user(user)

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

	handler_data := HandlerData{session: session, channel: channel, interaction: interaction, interaction_data: interaction_data, user: user}

	switch interaction.Type {
	case discordgo.InteractionApplicationCommand:
		fmt.Println("interaction", interaction_data.Name, "interaction", interaction, "From ", user.Username)
		commandHandlers[interaction_data.Name](handler_data)
	case discordgo.InteractionApplicationCommandAutocomplete:
		focused := 0
		for {
			if interaction_data.Options[focused].Focused {
				break
			}
			focused += 1
		}

		values := option_choice{}

		switch commandAutocomplete[interaction_data.Name][focused] {
		case AutoCompleteNone:
			return
		case AutoCompleteAllAccounts:
			values = make(option_choice, len(data.PersonalAccounts)+len(data.OrganisationAccounts))
			index := 0
			for id, account := range data.PersonalAccounts {
				values[index] = &discordgo.ApplicationCommandOptionChoice{Name: account.Name + " (Personal)", Value: id}
				index++
			}
			for id, account := range data.OrganisationAccounts {
				values[index] = &discordgo.ApplicationCommandOptionChoice{Name: account.Name + " (Organisation)", Value: id}
				index++
			}

		case AutoCompleteOwnedOrgs:
			values = make(option_choice, len(data.Users[user.ID].Organisations))
			index := 0
			for _, org := range data.Users[user.ID].Organisations {
				values[index] = &discordgo.ApplicationCommandOptionChoice{Name: data.OrganisationAccounts[org].Name + " (Organisation)", Value: org}
				index++
			}
		case AutoCompleteNonSelfUsers:
			index := 0
			values = make(option_choice, len(data.PersonalAccounts)-1)
			for id, other_user := range data.Users {
				if id != user.ID {
					values[index] = &discordgo.ApplicationCommandOptionChoice{Name: data.PersonalAccounts[other_user.PersonalAccount].Name + " (Person)", Value: id}
					index++
				}
			}
		}

		if len(values) > 0 {
			matches := fuzzy.FindFrom(interaction_data.Options[focused].Value.(string), values)
			results := make(option_choice, len(matches))
			for i, y := range matches {
				results[i] = values[y.Index]
			}
			if len(matches) == 0 {
				results = values
			}

			err = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionApplicationCommandAutocompleteResult,
				Data: &discordgo.InteractionResponseData{
					Choices: results,
				},
			})
			if err != nil {
				panic(err)
			}
		}

	}
}

func main() {
	// Create a new Discord session using the provided bot token.
	session, err := discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	// Register the interaction func as a callback for InteractionCreate events.
	session.AddHandler(interactionCreate)

	// Start checking if wealth tax should be applied
	go check_wealth_tax(session)

	// Only dms
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
