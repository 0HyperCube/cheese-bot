package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sahilm/fuzzy"
)

type Loan struct {
	Start     time.Time
	AmountDue int
	LoanValue int
	Warning   bool
	Overdue   bool
}

type User struct {
	PersonalAccount   string
	SuperUser         bool
	BankHolidaySetter bool
	Mp                bool
	LastPay           time.Time
	Organisations     []string
}

type Account struct {
	Name    string
	Balance int
	Loans   []*Loan
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
	BankHolidays         []int64
	LoanInterest         float64
	CasinoReturns        float64
}

const (
	AutoCompleteNonSelfUsers int8 = iota
	AutoCompleteUsers
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
	treasury string
	bank     string
	casino   string

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
				{
					Name:   "/sudo_set_bank_holiday",
					Value:  "Set a day to be a bank holiday or no longer a bank holiday.",
					Inline: false,
				},
				{
					Name:   "/bank_holidays",
					Value:  "List the bank holidays coming up soon.",
					Inline: false,
				},
				{
					Name:   "/sudo_loan",
					Value:  "Loans a account an unrestricted amount of cheesecoin. Can only be done by super user (i.e. head of bank).",
					Inline: false,
				},
				{
					Name:   "/sudo_set_interest_rate",
					Value:  "Sets interest rate in percent. Can only be done by super user (i.e. head of bank).",
					Inline: false,
				},
				{
					Name:   "/view_bank_loans",
					Value:  "View all the loans you have taken. The head of the bank can see all loans.",
					Inline: false,
				},
				{
					Name:   "/gamble",
					Value:  "Gamble [cheesecoins] on [number] being roled (1-6 dice)",
					Inline: false,
				}, {
					Name:   "/gambling_set_returns",
					Value:  "Set the returns on the gambling. Only avaliable to the owner of the casino.",
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
					create_embed("Payment", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own the ", payer_name, " organisation"), []*discordgo.MessageEmbedField{})
					return
				}
			}

			sucsess, err, tax := transaction(amount, payer_account, recipiant_account, payer_name, data_handler.session, nil)
			if !sucsess {
				create_embed("Payment", data_handler.session, data_handler.interaction, err, []*discordgo.MessageEmbedField{})
				return
			}

			create_embed("Payment", data_handler.session, data_handler.interaction, fmt.Sprint("Sucsessfully transfered ", format_cheesecoins(amount), " from ", payer_name, " to ", recipiant_name,
				".\n```\nAmount Payed    ", format_cheesecoins(amount), "\nTax           - ", format_cheesecoins(tax), "\nRecieved      = ", format_cheesecoins(amount-tax), "\n```", err),
				[]*discordgo.MessageEmbedField{})
		},
		"transfer_org": func(data_handler HandlerData) {
			// Get the organisation
			organisation := data_handler.interaction_data.Options[0].StringValue()
			organisation_name := data.OrganisationAccounts[organisation].Name

			if !user_has_org(data_handler.user, organisation, true) {
				create_embed("Transfer organisation", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own ", organisation_name), []*discordgo.MessageEmbedField{})
				return
			}

			// Get the recipiant
			recipiant := data.Users[data_handler.interaction_data.Options[1].StringValue()]
			recipiant_name := data.PersonalAccounts[recipiant.PersonalAccount].Name

			recipiant.Organisations = append(recipiant.Organisations, organisation)

			create_embed("Transfer organisation", data_handler.session, data_handler.interaction, fmt.Sprint(
				"Sucessfully transfered ", organisation_name, " to ", recipiant_name), []*discordgo.MessageEmbedField{})
		},
		"create_org": func(data_handler HandlerData) {
			name := data_handler.interaction_data.Options[0].StringValue()

			data.Users[data_handler.user.ID].Organisations = append(data.Users[data_handler.user.ID].Organisations, fmt.Sprint(data.NextOrg))
			data.OrganisationAccounts[fmt.Sprint(data.NextOrg)] = &Account{Name: name, Balance: 0, Loans: []*Loan{}}
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
			if duration.Hours() < 15 {
				create_embed("Rollcall", data_handler.session, data_handler.interaction, fmt.Sprint("You can claim this benefit only once per day. You have last claimed it ", duration.Round(time.Second).String(), " ago"),
					[]*discordgo.MessageEmbedField{})
				return
			}

			cheese_user.LastPay = time.Now()

			sucsess, err, _ := transaction(data.MpPay, data.OrganisationAccounts[treasury], data.PersonalAccounts[cheese_user.PersonalAccount], "Treasury", data_handler.session, data_handler.interaction)
			if !sucsess {
				create_embed("Rollcall", data_handler.session, data_handler.interaction, err, []*discordgo.MessageEmbedField{})
			}
		},
		"rename_org": func(data_handler HandlerData) {
			// Get the organisation
			organisation := data_handler.interaction_data.Options[0].StringValue()
			organisation_account := data.OrganisationAccounts[organisation]
			organisation_name := organisation_account.Name

			if !user_has_org(data_handler.user, organisation, false) {
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
			organisation := data_handler.interaction_data.Options[0].StringValue()
			organisation_name := data.OrganisationAccounts[organisation].Name

			if !user_has_org(data_handler.user, organisation, true) {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, fmt.Sprint("**ERROR:** You do not own ", organisation_name), []*discordgo.MessageEmbedField{})
				return
			}

			if organisation == treasury {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, "**ERROR:** You cannot delete the treasury!", []*discordgo.MessageEmbedField{})
				return
			}

			if organisation == bank {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, "**ERROR:** You cannot delete the bank!", []*discordgo.MessageEmbedField{})
				return
			}

			if organisation == casino {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, "**ERROR:** You cannot delete the casino! It is to important.", []*discordgo.MessageEmbedField{})
				return
			}

			org_account := data.OrganisationAccounts[organisation]
			sucsess, err, tax := transaction(org_account.Balance, org_account, user_account, "destroyed organisation", data_handler.session, nil)
			if !sucsess {
				create_embed("Delete organisation", data_handler.session, data_handler.interaction, err, []*discordgo.MessageEmbedField{})
				return
			}

			delete(data.OrganisationAccounts, organisation)

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
		"sudo_set_bank_holiday": func(data_handler HandlerData) {
			if !data.Users[data_handler.user.ID].BankHolidaySetter {
				create_embed("Set Bank Holiday", data_handler.session, data_handler.interaction, "**ERROR:** You are not a user eligible to set bank holidays", []*discordgo.MessageEmbedField{})
				return
			}
			day := data_handler.interaction_data.Options[0].IntValue()
			month := data_handler.interaction_data.Options[1].IntValue()
			enabled := data_handler.interaction_data.Options[2].BoolValue()
			holiday := month<<12 + day
			contains_time := contains_int64(data.BankHolidays, holiday)

			result := ""
			if enabled {
				if contains_time {
					result = "is already a bank holiday"

				} else {
					result = "is now a bank holiday"
					data.BankHolidays = append(data.BankHolidays, holiday)
				}
			} else {
				if contains_time {
					result = "is no longer a bank holiday"
					data.BankHolidays = remove_int64(data.BankHolidays, holiday)
				} else {
					result = "was already not a bank holiday"
				}

			}

			create_embed("Set Bank Holiday", data_handler.session, data_handler.interaction, fmt.Sprint(format_date(holiday), " ", result, "."), []*discordgo.MessageEmbedField{})
		},
		"bank_holidays": func(data_handler HandlerData) {

			result := ""
			if len(data.BankHolidays) > 0 {
				for _, t := range data.BankHolidays {
					result += fmt.Sprint("\nBank holiday on ", format_date(t), " ", result)
				}
				fmt.Print("result: ", result)
			} else {
				result += "No Bank holidays."
			}

			create_embed("Bank Holidays", data_handler.session, data_handler.interaction, result, []*discordgo.MessageEmbedField{})
		},
		"sudo_loan": func(data_handler HandlerData) {
			if !user_has_org(data_handler.user, bank, false) {
				create_embed("Loan", data_handler.session, data_handler.interaction, "**ERROR:** You are not a super user", []*discordgo.MessageEmbedField{})
				return
			}

			recipiant := data_handler.interaction_data.Options[0].StringValue()
			recipiant_account, _ := get_account(recipiant)

			// Get the transaction amount
			float_amount, _ := data_handler.interaction_data.Options[1].Value.(float64)
			amount := int(float_amount * 100)

			sucsess, result, _ := transaction(amount, data.OrganisationAccounts[bank], recipiant_account, "The Bank", data_handler.session, nil)

			if sucsess {
				result = fmt.Sprint("A ", format_cheesecoins(amount), " loan has been granted to ", recipiant_account.Name, " with an interest rate of ", fmt.Sprintf("%.2f%%", data.LoanInterest), ".")
				recipiant_account.Loans = append(recipiant_account.Loans, &Loan{Start: time.Now(), AmountDue: int(math.Ceil(float64(amount) * (data.LoanInterest + 100) / 100)), LoanValue: amount})
			}

			create_embed("Loan", data_handler.session, data_handler.interaction, result, []*discordgo.MessageEmbedField{})
		},
		"sudo_set_interest_rate": func(data_handler HandlerData) {
			if !user_has_org(data_handler.user, bank, false) {
				create_embed("Set Interest Rate", data_handler.session, data_handler.interaction, "**ERROR:** You are not a super user", []*discordgo.MessageEmbedField{})
				return
			}

			data.LoanInterest = data_handler.interaction_data.Options[0].Value.(float64)

			create_embed("Set Interest Rate", data_handler.session, data_handler.interaction, fmt.Sprint("Sucessfully set interest rate to ", data.LoanInterest, "%."), []*discordgo.MessageEmbedField{})
		},
		"view_bank_loans": func(data_handler HandlerData) {
			cheese_user := data.Users[data_handler.user.ID]

			result := "**Your loans:**"
			r, any_loans := format_loans(data.PersonalAccounts[cheese_user.PersonalAccount])
			result += r
			for _, org := range cheese_user.Organisations {
				r, loan := format_loans(data.OrganisationAccounts[org])
				result += r
				if loan {
					any_loans = true
				}
			}
			if !any_loans {
				result += "\nNo loans."
			}

			if user_has_org(data_handler.user, bank, false) {
				result += "\n\n**All loans:**"
				for _, acc := range data.PersonalAccounts {
					r, loan := format_loans(acc)
					result += r
					if loan {
						any_loans = true
					}
				}
				for _, acc := range data.OrganisationAccounts {
					r, loan := format_loans(acc)
					result += r
					if loan {
						any_loans = true
					}
				}
				if !any_loans {
					result += "\nNo loans."
				}
			}

			create_embed("View Loans", data_handler.session, data_handler.interaction, result, []*discordgo.MessageEmbedField{})
		},
		"gamble": func(data_handler HandlerData) {
			// Get the transaction amount
			float_amount, _ := data_handler.interaction_data.Options[0].Value.(float64)
			amount := int(float_amount * 100)

			cheese_account := data.PersonalAccounts[data.Users[data_handler.user.ID].PersonalAccount]

			if amount > cheese_account.Balance {
				create_embed("Gamble", data_handler.session, data_handler.interaction, "**ERROR:** You do not have enough funds.", []*discordgo.MessageEmbedField{})
				return
			}

			winnings := int(float64(amount) * (data.CasinoReturns - 1))

			if winnings > data.OrganisationAccounts[casino].Balance {
				create_embed("Gamble", data_handler.session, data_handler.interaction, "**ERROR:** The casino does not have enough funds.", []*discordgo.MessageEmbedField{})
				return
			}

			// Get the dice
			predicted_dice := int(data_handler.interaction_data.Options[1].IntValue())
			actual_dice := rand.Intn(5) + 1

			title := "Gambling Loss"
			description := ""
			if predicted_dice == actual_dice {
				title = "Gambling Victory"

				description = fmt.Sprint("You predicted a 🎲", predicted_dice, " and the computer rolled a 🎲", actual_dice, ". You have won ", format_cheesecoins(winnings), " which will be transfered to your account shortly.")
				transaction(winnings, data.OrganisationAccounts[casino], cheese_account, "Casino", data_handler.session, nil)
			} else {
				description = fmt.Sprint("You predicted a 🎲", predicted_dice, " and the computer rolled a 🎲", actual_dice, ". You have lost ", format_cheesecoins(amount), ".")
				transaction(amount, cheese_account, data.OrganisationAccounts[casino], cheese_account.Name, data_handler.session, nil)
			}

			create_embed(title, data_handler.session, data_handler.interaction, description, []*discordgo.MessageEmbedField{})

		},
		"gambling_set_returns": func(data_handler HandlerData) {
			if !user_has_org(data_handler.user, casino, false) {
				create_embed("Gambling Set Returns", data_handler.session, data_handler.interaction, "**ERROR:** You are not the casino owner.", []*discordgo.MessageEmbedField{})
				return
			}

			data.CasinoReturns = data_handler.interaction_data.Options[0].Value.(float64)

			create_embed("Gambling Set Returns", data_handler.session, data_handler.interaction, fmt.Sprint("Sucessfully set gambling returns to ", data.CasinoReturns, "."), []*discordgo.MessageEmbedField{})
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
		"sudo_set_bank_holiday":    {AutoCompleteNone, AutoCompleteNone, AutoCompleteNone},
		"bank_holidays":            {},
		"sudo_loan":                {AutoCompleteAllAccounts, AutoCompleteNone},
		"sudo_set_interest_rate":   {AutoCompleteNone},
		"view_bank_loans":          {},
		"gamble":                   {AutoCompleteNone, AutoCompleteNone},
		"gambling_set_returns":     {AutoCompleteNone},
	}
)

func format_loans(account *Account) (string, bool) {

	if len(account.Loans) > 0 {
		result := ""
		for _, t := range account.Loans {
			result += fmt.Sprintf("\n**%s** has a loan of **%s** due on %s. **%s** is yet to be paid", account.Name, format_cheesecoins(t.LoanValue), fmt.Sprint("<t:", t.Start.AddDate(0, 0, 7).Unix(), ":f>"), format_cheesecoins(t.AmountDue))
		}
		return result, true
	} else {
		return "", false
	}

}

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
		}, {
			Name:        "sudo_set_bank_holiday",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Set a day to be a bank holiday or no longer a bank holiday.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "day",
					Description: "Day of the month of the bank holiday. e.g. for 2/12/21 2 is the second day of the month",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "month",
					Description: "Month of the bank holiday.",
					Required:    true,
					Choices:     months_choices(),
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "enabled",
					Description: "If it should be a bank holiday.",
					Required:    true,
				},
			},
		}, {
			Name:        "bank_holidays",
			Type:        discordgo.ChatApplicationCommand,
			Description: "List the bank holidays coming up soon.",
		}, {
			Name:        "sudo_loan",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Loans an account some cheesecoin. Can only be done by super user (i.e. head of bank).",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "recipiant",
					Description:  "Recipiant of the loan",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
					Name:        "amount",
					Description: "Amount to loan.",
					Required:    true,
				},
			},
		}, {
			Name:        "sudo_set_interest_rate",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Sets interest rate in percent. Can only be done by super user (i.e. head of bank).",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionType(10), // Float
				Name:        "new_interest",
				Description: "The new interest rate (0% to 100%).",
				Required:    true,
			}},
		}, {
			Name:        "view_bank_loans",
			Type:        discordgo.ChatApplicationCommand,
			Description: "View all the loans you have taken. The head of the bank can see all loans.",
		}, {
			Name:        "gamble",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Gamble [cheesecoins] on [number] being roled (1-6 dice) at the casino.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionType(10), // Float
					Name:        "cheesecoin",
					Description: "Amount of cheesecoins",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "number",
					Description: "The number you are betting on.",
					Required:    true,
					Choices:     dice_choices(),
				},
			},
		}, {
			Name:        "gambling_set_returns",
			Type:        discordgo.ChatApplicationCommand,
			Description: "Set the returns on the gambling. Only avaliable to the owner of the casino.",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionType(10), // Float
				Name:        "returns",
				Description: "Returns from win.",
				Required:    true,
			}},
		},
	}

	_, err := session.ApplicationCommandBulkOverwrite(session.State.User.ID, "", command)

	if err != nil {
		log.Fatal(err)
	}
}

// Checks if a time is in a list of times
func contains_int64(s []int64, value int64) bool {
	for _, v := range s {
		if v == value {
			return true
		}
	}
	return false
}

// Removes a time from an array of times
func remove_int64(s []int64, value int64) []int64 {
	result := []int64{}
	for _, v := range s {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

// Converts a date into a month and a day
func parse_date(value int64) (int64, int64) {
	month := value >> 12
	day := value - (month << 12)

	return month, day
}

// Formats a date in the day/month/year format
func format_date(value int64) string {
	month, day := parse_date(value)
	return fmt.Sprint(day, "/", month, "/", time.Now().Year())
}

func day_is_date(date int64, current_time time.Time) bool {
	month, day := parse_date(date)
	return current_time.Month() == time.Month(month) && current_time.Day() == int(day)
}

// Generates a command option choice for all the months of the year with values starting at 1.
func months_choices() []*discordgo.ApplicationCommandOptionChoice {
	result := make([]*discordgo.ApplicationCommandOptionChoice, 12)
	for i, v := range []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"} {
		result[i] = &discordgo.ApplicationCommandOptionChoice{Name: v, Value: i + 1}
	}
	return result
}

// Generates a command option choice for numbers from 1 - 6
func dice_choices() []*discordgo.ApplicationCommandOptionChoice {
	result := make([]*discordgo.ApplicationCommandOptionChoice, 6)
	for i := 0; i < 6; i++ {
		result[i] = &discordgo.ApplicationCommandOptionChoice{Name: fmt.Sprint(i + 1), Value: i + 1}
	}
	return result
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

	// Assign special organisations
	treasury = "1000"
	bank = "1003"
	casino = "1023"
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
	data.OrganisationAccounts[treasury].Balance += tax
	return fmt.Sprintf("\n%-20s %s", name+":", format_cheesecoins(tax))
}

// Applies wealth tax. Called every day
func apply_wealth_tax(session *discordgo.Session) {
	fmt.Print("Wealth tax.")
	for id, usr := range data.Users {
		result := apply_wealth_tax_account(data.PersonalAccounts[usr.PersonalAccount], "Personal")
		for _, org := range usr.Organisations {
			account := data.OrganisationAccounts[org]
			if account != data.OrganisationAccounts[treasury] {
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

// Adds timers for callbacks on overdue loans.
func loan_callbacks(session *discordgo.Session) {
	for _, acc := range data.PersonalAccounts {
		user := account_owner(acc)
		for _, loan := range acc.Loans {
			loan_end := loan.Start.Add(time.Hour * 24 * 7).Unix()
			banker := account_owner(data.OrganisationAccounts[bank])
			if !loan.Warning {
				time.AfterFunc(time.Until(loan.Start.AddDate(0, 0, 5)), func() {
					loan.Warning = true
					send_embed("Loan Due", session, user, fmt.Sprint("Your loan of ", format_cheesecoins(loan.LoanValue), " is due <t:", loan_end, ":R>. ", format_cheesecoins(loan.AmountDue), " is yet to be paid."), []*discordgo.MessageEmbedField{})
					user_name := data.PersonalAccounts[data.Users[user].PersonalAccount].Name
					send_embed(fmt.Sprint(user_name, " has a loan due"), session, banker, fmt.Sprint(user_name, " has a loan of ", format_cheesecoins(loan.LoanValue), " which is due <t:", loan_end, ":R>. ", format_cheesecoins(loan.AmountDue), " is yet to be paid."), []*discordgo.MessageEmbedField{})

				})
			}
			if !loan.Overdue {
				time.AfterFunc(time.Until(loan.Start.AddDate(0, 0, 7)), func() {
					loan.Overdue = true
					loan.Warning = true
					send_embed("Loan Overdue", session, user, fmt.Sprint("Your loan of ", format_cheesecoins(loan.LoanValue), " should have been paid <t:", loan_end, ":R> but ", format_cheesecoins(loan.AmountDue), " is yet to be paid. The bank has been notified and may take legal action."), []*discordgo.MessageEmbedField{})
					user_name := data.PersonalAccounts[data.Users[user].PersonalAccount].Name
					send_embed(fmt.Sprint(user_name, " has an overdue loan"), session, banker, fmt.Sprint(user_name, " has a loan of ", format_cheesecoins(loan.LoanValue), " due <t:", loan_end, ":R> but ", format_cheesecoins(loan.AmountDue), " is yet to be paid. Take any legal action you consider necessary."), []*discordgo.MessageEmbedField{})
				})
			}
		}
	}
}

// Called as the first function to run from this module
func init() {
	rand.Seed(time.Now().UnixNano())

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
		data.PersonalAccounts[fmt.Sprint(data.NextPersonal)] = &Account{Name: user.Username, Balance: 0, Loans: []*Loan{}}
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

	// Handle paying back a loan
	loan_text := ""
	if recipiant_account == data.OrganisationAccounts[bank] {
		fmt.Println("Paying bank.")
		if len(payer_account.Loans) > 0 {
			fmt.Println("Paying loan.")
			loan_text += "\n\n**Loan contributions**:"
			amount_left := amount - tax
			for {
				if amount_left >= payer_account.Loans[0].AmountDue {
					loan_text += fmt.Sprint("\n", format_cheesecoins(payer_account.Loans[0].AmountDue), " payed off a loan of ", format_cheesecoins(payer_account.Loans[0].LoanValue), " from <t:", payer_account.Loans[0].Start.Unix(), ":f>")
					amount_left -= payer_account.Loans[0].AmountDue
					payer_account.Loans = payer_account.Loans[1:]
				} else {
					if amount_left > 0 {
						loan_text += fmt.Sprint("\n", format_cheesecoins(amount_left), " towards a loan of ", format_cheesecoins(payer_account.Loans[0].LoanValue), " from <t:", payer_account.Loans[0].Start.Unix(), ":f>. ", format_cheesecoins(payer_account.Loans[0].LoanValue-amount_left), " is remaining from this loan.")
						payer_account.Loans[0].AmountDue -= amount_left
						amount_left = 0
					}
					break
				}
				if len(payer_account.Loans) == 0 {
					break
				}
			}
		}
	}
	fmt.Println("loan text", loan_text)

	payer_account.Balance -= amount
	recipiant_account.Balance += amount - tax
	data.OrganisationAccounts[treasury].Balance += tax

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

	return true, loan_text, tax
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

		if interaction_data.Name != "sudo_set_bank_holiday" && interaction_data.Name != "bank_holidays" {
			for _, t := range data.BankHolidays {
				if day_is_date(t, time.Now()) {
					create_embed("Bank Holiday!", handler_data.session, handler_data.interaction, "Today is a bank holiday so no banking must be done.", []*discordgo.MessageEmbedField{})
					return
				}
			}
		}
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
		case AutoCompleteUsers:
			index := 0
			values = make(option_choice, len(data.PersonalAccounts))
			for id, other_user := range data.Users {
				values[index] = &discordgo.ApplicationCommandOptionChoice{Name: data.PersonalAccounts[other_user.PersonalAccount].Name + " (Person)", Value: id}
				index++
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

	// Messages on late loans
	loan_callbacks(session)

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

	stop := make(chan os.Signal, 2)
	signal.Notify(stop, os.Interrupt)
	<-stop
	fmt.Println("Closing connection")
	save_data()
	// Cleanly close down the Discord session.
	session.Close()

}
