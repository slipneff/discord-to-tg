package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
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
	DiscordToken   string
	TelegramToken  string
	DiscordIDs     []string
	TelegramChatID map[int64]bool
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

	discordChannels := map[string]bool{}
	for _, v := range config.DiscordIDs {
		discordChannels[v] = true
	}
	discord, err := discordgo.New("Bot " + config.DiscordToken)
	if err != nil {
		log.Fatal("Error creating Discord session: ", err)
	}

	telegram, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		log.Fatal("Error creating Telegram bot: ", err)
	}

	discordMessages := make(chan *discordgo.MessageCreate)

	err = discord.Open()
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}
	defer discord.Close()

	discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if discordChannels[m.ChannelID] {
			discordMessages <- m
		}
	})
	log.Println("Started without errors")
	go func() {
		for {
			select {
			case msg := <-discordMessages:
				channel, err := discord.Channel(msg.ChannelID)
				if err != nil {
					log.Fatal("Error retrieving channel information: ", err)
				}
				guild, err := discord.Guild(channel.GuildID)
				if err != nil {
					log.Fatal("Error retrieving guild information: ", err)
				}

				sendMessageToTelegram(config, telegram, Message{Content: msg.Content, ChannelName: channel.Name, GuildName: guild.Name})
			}
		}
	}()
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, _ := telegram.GetUpdatesChan(u)
	for update := range updates {
		if update.Message != nil {
			chatID := update.Message.Chat.ID
			if !config.TelegramChatID[chatID] {
				msg := tgbotapi.NewMessage(chatID, "Chat registered")

				telegram.Send(msg)
			}
			config.TelegramChatID[chatID] = true

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
