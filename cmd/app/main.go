package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/spf13/viper"
	tgbotapi "gopkg.in/telegram-bot-api.v4"
)

type Message struct {
	Content     string
	GuildName   string
	ChannelName string
}

func (m *Message) ToString() string {
	return fmt.Sprintf("[%s/%s] %s", m.GuildName, m.ChannelName, m.Content)
}

type Config struct {
	TelegramToken  string
	Login          string
	Password       string
	TelegramChatID map[int64]bool
	AllMessages    bool
}

func LoadConfig(path string) (*Config, error) {
	config := new(Config)

	viper.SetConfigFile(path)

	err := viper.ReadInConfig()
	if err != nil {
		return nil, err
	}

	err = viper.Unmarshal(config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func MustLoadConfig(path string) *Config {
	config, err := LoadConfig(path)
	if err != nil {
		panic(err)
	}
	config.TelegramChatID = make(map[int64]bool)

	return config
}

func main() {
	config := MustLoadConfig("config.yaml")
	ctx := context.Background()
	discordChannels := map[string]bool{}
	discord, err := session.Login(ctx, config.Login, config.Password, "")
	if err != nil {
		log.Fatalf("Ошибка при создании сессии Discord: %v", err)
	}

	telegram, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		log.Fatal("Error creating Telegram bot: ", err)
	}

	err = discord.Open(ctx)
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}
	defer discord.Close()

	discord.AddHandler(func(m *gateway.MessageCreateEvent) {
		if config.AllMessages || discordChannels[m.ChannelID.String()] {
			channel, err := discord.Channel(m.ChannelID)
			if err != nil {
				log.Fatal("Error retrieving channel information: ", err)
			}
			guild, err := discord.Guild(channel.GuildID)
			if err != nil {
				log.Fatal("Error retrieving guild information: ", err)
			}

			sendMessageToTelegram(config, telegram, Message{Content: m.Content, ChannelName: channel.Name, GuildName: guild.Name})
		}
	})

	log.Println("Started without errors")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates, _ := telegram.GetUpdatesChan(u)
	for update := range updates {
		if !config.AllMessages {
			message := strings.Split(update.Message.Text, " ")
			if len(message) > 1 {
				if message[0] == "/add" {
					discordChannels[message[1]] = true
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Discord channel registered")
					telegram.Send(msg)
				}
				if message[0] == "/remove" {
					discordChannels[message[1]] = false
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Discord channel unregistered")
					telegram.Send(msg)
				}
			}
		}

		if update.Message != nil {
			chatID := update.Message.Chat.ID
			if !config.TelegramChatID[chatID] {
				msg := tgbotapi.NewMessage(chatID, "Chat registered")
				config.TelegramChatID[chatID] = true
				telegram.Send(msg)
			}
		}
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt
	log.Println("Shutting down...")
}

func sendMessageToTelegram(cfg *Config, bot *tgbotapi.BotAPI, content Message) {
	for key, value := range cfg.TelegramChatID {
		if value {
			msg := tgbotapi.NewMessage(key, content.ToString())
			_, err := bot.Send(msg)
			if err != nil {
				log.Println("Error sending message to Telegram: ", err)
			}
		}
	}

}
