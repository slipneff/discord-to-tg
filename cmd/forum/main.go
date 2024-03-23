package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
	tgbotapi "gopkg.in/telegram-bot-api.v4"
)

func connectDB() (*sqlx.DB, error) {
	// Открываем базу данных
	db, err := sqlx.Connect("sqlite3", "bot_forum.db")
	if err != nil {
		return nil, err
	}

	// Создаем таблицу для хранения конфигурации
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS configuration (
		chat_id INTEGER,
		guild_id TEXT,
		first BOOLEAN,
		PRIMARY KEY (chat_id, guild_id)
	)`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

type Message struct {
	Content     string
	GuildName   string
	ChannelName string
	ChatID      int64
}

func (m *Message) ToString() string {
	return fmt.Sprintf("[%s/%s] %s", m.GuildName, m.ChannelName, m.Content)
}

type Config struct {
	TelegramTokenForum string
	DiscordTokenForum  string
	TelegramChatID     map[int64][]string
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
	config.TelegramChatID = make(map[int64][]string)

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

	discord := session.New(config.DiscordTokenForum)

	telegram, err := tgbotapi.NewBotAPI(config.TelegramTokenForum)
	if err != nil {
		log.Fatal("Error creating Telegram bot: ", err)
	}

	err = discord.Open(ctx)
	if err != nil {
		log.Fatal("Error opening Discord session: ", err)
	}
	defer discord.Close()

	discord.AddHandler(func(m *gateway.MessageCreateEvent) {
		err := discord.JoinThread(m.ChannelID)
		if err == nil {
			guilds, _ := getChatByChannelID(db, m.GuildID.String())
			for _, guildID := range guilds {
				if strings.Contains(m.Message.Content, "@everyone") || isFirstMessageInChannel(discord, m.ChannelID, m.ID) {
					channel, err := discord.Channel(m.ChannelID)
					if err != nil {
						log.Fatal("Error retrieving channel information: ", err)
					}
					guild, err := discord.Guild(channel.GuildID)
					if err != nil {
						log.Fatal("Error retrieving guild information: ", err)
					}
					message := Message{Content: m.Content, ChannelName: channel.Name, GuildName: guild.Name, ChatID: guildID}
					fmt.Println(message)
					sendMessageToTelegram(telegram, message)
				}
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

			fmt.Println(update.Message.Chat.ID)
			if !isChatRegistered(db, update.Message.Chat.ID) {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Chat registered")
				telegram.Send(msg)
				registerChat(db, update.Message.Chat.ID)
			}
		}
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt
	log.Println("Shutting down...")
}

func sendMessageToTelegram(bot *tgbotapi.BotAPI, content Message) {
	msg := tgbotapi.NewMessage(content.ChatID, content.ToString())
	_, err := bot.Send(msg)
	if err != nil {
		log.Println("Error sending message to Telegram: ", err)
	}
}
func isFirstMessageInChannel(discord1 *session.Session, channelID discord.ChannelID, messageID discord.MessageID) bool {
	// Получаем список сообщений в канале
	messages, err := discord1.MessagesBefore(channelID, messageID, 10)
	if err != nil {
		log.Printf("Ошибка при получении сообщений из канала: %v", err)
		return false
	}
	if len(messages) == 0 {
		return true
	}

	return false
}

func isChatRegistered(db *sqlx.DB, chatID int64) bool {
	var count int
	db.Get(&count, "SELECT COUNT(*) FROM configuration WHERE (chat_id) = ?", chatID)
	return count > 0
}

func registerChat(db *sqlx.DB, chatID int64) error {
	_, err := db.Exec("INSERT INTO configuration (chat_id, guild_id) VALUES (?, ?)", chatID, "register")
	return err
}

func addChannel(db *sqlx.DB, chatID int64, guildID string) error {
	_, err := db.Exec("INSERT INTO configuration (chat_id, guild_id) VALUES (?, ?)", chatID, guildID)
	return err
}

func removeChannel(db *sqlx.DB, chatID int64, guildID string) error {
	_, err := db.Exec("DELETE FROM configuration WHERE (chat_id) = ? AND (guild_id) = ?", chatID, guildID)
	return err
}

func getChatByChannelID(db *sqlx.DB, guildID string) ([]int64, error) {
	var chatID []int64
	err := db.Select(&chatID, "SELECT chat_id FROM configuration WHERE guild_id = ?", guildID)
	return chatID, err
}
