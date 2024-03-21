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
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
	tgbotapi "gopkg.in/telegram-bot-api.v4"
)

type Message struct {
	Content     string
	GuildName   string
	ChannelName string
	ChannelID   string
}

func (m *Message) ToString() string {
	return fmt.Sprintf("[%s/%s] %s", m.GuildName, m.ChannelName, m.Content)
}

type Config struct {
	TelegramTokenParser string
	DiscordTokenParser  string
}

func connectDB() (*sqlx.DB, error) {
	// Открываем базу данных
	db, err := sqlx.Connect("sqlite3", "bot_parser.db")
	if err != nil {
		return nil, err
	}

	// Создаем таблицу для хранения конфигурации
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS configuration (
		chat_id INTEGER,
		channel_id TEXT,
		PRIMARY KEY (chat_id, channel_id)
	)`)
	if err != nil {
		return nil, err
	}

	return db, nil
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

	return config
}

func main() {
	config := MustLoadConfig("config.yaml")
	ctx := context.Background()

	db, err := connectDB()
	if err != nil {
		log.Fatalf("Ошибка при подключении к базе данных: %v", err)
	}
	defer db.Close()

	discord := session.New(config.DiscordTokenParser)

	telegram, err := tgbotapi.NewBotAPI(config.TelegramTokenParser)
	if err != nil {
		log.Fatal("Error creating Telegram bot: ", err)
	}

	err = discord.Open(ctx)
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}
	defer discord.Close()

	discord.AddHandler(func(m *gateway.MessageCreateEvent) {
		channels, _ := getChannels(db)
		for _, channel := range channels {
			if channel == m.ChannelID.String() {
				channel, err := discord.Channel(m.ChannelID)
				if err != nil {
					log.Fatal("Error retrieving channel information: ", err)
				}
				guild, err := discord.Guild(channel.GuildID)
				if err != nil {
					log.Fatal("Error retrieving guild information: ", err)
				}

				sendMessageToTelegram(db, telegram, Message{Content: m.Content, ChannelName: channel.Name, GuildName: guild.Name, ChannelID: m.ChannelID.String()})
			}
		}
	})

	log.Println("Started without errors")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates, _ := telegram.GetUpdatesChan(u)
	for update := range updates {
		if update.Message != nil {
			message := strings.Split(update.Message.Text, " ")
			if len(message) > 1 {
				if message[0] == "/add" {
					addChannel(db, update.Message.Chat.ID, message[1])
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Discord channel registered")
					telegram.Send(msg)
				}
				if message[0] == "/remove" {
					removeChannel(db, update.Message.Chat.ID, message[1])
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Discord channel unregistered")
					telegram.Send(msg)
				}
			}

			chatID := update.Message.Chat.ID
			if !isChatRegistered(db, chatID) {
				msg := tgbotapi.NewMessage(chatID, "Chat registered")
				telegram.Send(msg)
				registerChat(db, chatID)
			}
		}
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt
	log.Println("Shutting down...")
}

func sendMessageToTelegram(db *sqlx.DB, bot *tgbotapi.BotAPI, content Message) {
	chat, _ := getChatByChannelID(db, content.ChannelID)
	msg := tgbotapi.NewMessage(chat, content.ToString())
	_, err := bot.Send(msg)
	if err != nil {
		log.Println("Error sending message to Telegram: ", err)
	}
}

func isChatRegistered(db *sqlx.DB, chatID int64) bool {
	var count int
	db.Get(&count, "SELECT COUNT(*) FROM configuration WHERE (chat_id) = ?", chatID)
	return count > 0
}

func registerChat(db *sqlx.DB, chatID int64) error {
	_, err := db.Exec("INSERT INTO configuration (chat_id, channel_id) VALUES (?, ?)", chatID, "register")
	return err
}

func addChannel(db *sqlx.DB, chatID int64, channelID string) error {
	_, err := db.Exec("INSERT INTO configuration (chat_id, channel_id) VALUES (?, ?)", chatID, channelID)
	return err
}

func removeChannel(db *sqlx.DB, chatID int64, channelID string) error {
	_, err := db.Exec("DELETE FROM configuration WHERE (chat_id) = ? AND (channel_id) = ?", chatID, channelID)
	return err
}
func getChannels(db *sqlx.DB) ([]string, error) {
	var chatIDs []string
	err := db.Select(&chatIDs, "SELECT (channel_id) FROM configuration")
	return chatIDs, err
}
func getChatByChannelID(db *sqlx.DB, channelID string) (int64, error) {
	var chatID int64
	err := db.Get(&chatID, "SELECT (chat_id) FROM configuration WHERE (channel_id) = ?", channelID)
	return chatID, err
}
