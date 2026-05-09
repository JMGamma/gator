package main

import (
	"bootdev-aggregator/internal/config"
	"bootdev-aggregator/internal/database"
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"time"
	"strconv"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type state struct {
	db *database.Queries
	config *config.Config
}

type command struct {
	Name string
	Args []string
}

type commands struct {
	handlers map[string]func(*state, command) error
}

type RSSFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Item        []RSSItem `xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func (c *commands) run(s *state, cmd command) error {
	val, ok := c.handlers[cmd.Name]
	if !ok {
		return fmt.Errorf("unknown command: %s", cmd.Name)
	} 
	return val(s, cmd)
}

func (c *commands) register(name string, f func(*state, command) error) {
	c.handlers[name] = f
}

func handlerLogin(s *state, cmd command) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("no username provided")
	}
	ctx := context.Background()
	_, err := s.db.FindUser(ctx, cmd.Args[0])
	if err != nil {
		return fmt.Errorf("user not found")
	}
	err = s.config.SetUser(cmd.Args[0])
	if err != nil {
		return fmt.Errorf("error setting user: %w", err)
	}
	fmt.Printf("User set to: %s\n", cmd.Args[0])
	return nil
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("no name provided")
	}
	ctx := context.Background()
	_, err := s.db.FindUser(ctx, cmd.Args[0])
	if err == nil {
		return fmt.Errorf("user already exists")
	}
	
	uuid, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("error generating UUID: %w", err)
	}
	now := time.Now()
	_, err = s.db.CreateUser(ctx, database.CreateUserParams{
		ID:        uuid,
		CreatedAt: now,
		UpdatedAt: now,
		Name:      cmd.Args[0],
	})
	if err != nil {
		return fmt.Errorf("error creating user: %w", err)
	}
	fmt.Printf("User %s registered successfully\n", cmd.Args[0])
	s.config.SetUser(cmd.Args[0])
	return nil
}

func feedsHandler(s *state, _ command) error {
	ctx := context.Background()
	feeds, err := s.db.GetFeeds(ctx)
	if err != nil {
		return fmt.Errorf("error getting feeds: %w", err)
	}
	fmt.Println("Feeds:")
	for _, feed := range feeds {
		fmt.Printf("- %s (%s) added by %s\n", feed.Name, feed.Url, feed.UserName)
	}
	return nil
}

func reset(s *state, cmd command) error {
	ctx := context.Background()
	err := s.db.ResetDb(ctx)
	if err != nil {
		return fmt.Errorf("error resetting database: %w", err)
	}
	fmt.Println("Database reset successfully")
	return nil
}

func users(s *state, cmd command) error {
	ctx := context.Background()
	usernames, err := s.db.GetUsers(ctx)
	if err != nil {
		return fmt.Errorf("error getting users: %w", err)
	}
	fmt.Println("Users:")
	for _, name := range usernames {
		if name == s.config.Current_user_name {
			fmt.Printf("- %s (current)\n", name)
			continue
		}
		fmt.Printf("- %s\n", name)
	}
	return nil
}

func agg(s *state, cmd command) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("usage: agg <time_between_reqs>")
	}
	interval, err := time.ParseDuration(cmd.Args[0])
	if err != nil {
		return fmt.Errorf("error parsing duration: %w", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fmt.Printf("Scraping feeds every %s...\n", interval)
			err := scrapeFeeds(s)
			if err != nil {
				fmt.Printf("Error scraping feeds: %v\n", err)
			}
		}
	}
	return nil
}

func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("User-Agent", "gator")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching feed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var feed RSSFeed
	err = xml.Unmarshal(data, &feed)
	if err != nil {
		return nil, fmt.Errorf("error decoding feed: %w", err)
	}
	feed.Channel.Title = html.UnescapeString(feed.Channel.Title)
	feed.Channel.Description = html.UnescapeString(feed.Channel.Description)
	for i := range feed.Channel.Item {
		feed.Channel.Item[i].Title = html.UnescapeString(feed.Channel.Item[i].Title)
		feed.Channel.Item[i].Description = html.UnescapeString(feed.Channel.Item[i].Description)
		feed.Channel.Item[i].PubDate = html.UnescapeString(feed.Channel.Item[i].PubDate)
	}

	return &feed, nil
}

func addfeed(s *state, cmd command, user database.User) error {
	if len(cmd.Args) < 2 {
		return fmt.Errorf("usage: addfeed <name> <url>")
	}
	ctx := context.Background()
	uuid, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("error generating UUID: %w", err)
	}
	now := time.Now()
	_, err = s.db.CreateFeed(ctx, database.CreateFeedParams{
		ID:        uuid,
		CreatedAt: now,
		UpdatedAt: now,
		Name:      cmd.Args[0],
		Url:       cmd.Args[1],
		UserID:    user.ID,
	})
	if err != nil {
		return fmt.Errorf("error creating feed: %w", err)
	}
	err = AddFeedFollow(s, command{Name: "addfeedfollow", Args: []string{cmd.Args[1]}}, user)
	if err != nil {
		return fmt.Errorf("error adding feed follow: %w", err)
	}
	fmt.Printf("Feed '%s' added successfully\n", cmd.Args[0])
	return nil
}

func AddFeedFollow(s *state, cmd command, user database.User) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("usage: addfeedfollow <url>")
	}
	ctx := context.Background()
	feed, err := s.db.GetFeedID(ctx, cmd.Args[0])
	if err != nil {
		return fmt.Errorf("feed not found: %w", err)
	}
	uuid, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("error generating UUID: %w", err)
	}
	_, err = s.db.CreateFeedFollow(ctx, database.CreateFeedFollowParams{
		ID: uuid,
		UserID: user.ID,
		FeedID: feed.ID,
	})
	if err != nil {
		return fmt.Errorf("error creating feed follow: %w", err)
	}
	fmt.Println("Feed follow added successfully")
	return nil
}

func following(s *state, _ command, user database.User) error {
	follows, err := s.db.GetFeedFollows(context.Background(), user.Name)
	if err != nil {
		return fmt.Errorf("error getting feed follows: %w", err)
	}
	fmt.Println("Following:")
	for _, follow := range follows {
		fmt.Printf("- %s \n", follow.Name)
	}
	return nil
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		user, err := s.db.FindUser(context.Background(), s.config.Current_user_name)
		if err != nil {
			return fmt.Errorf("user not logged in")
		}
		return handler(s, cmd, user)
	}
}

func unfollow(s *state, cmd command, user database.User) error {
	if len(cmd.Args) < 1 {
		return fmt.Errorf("usage: unfollow <url>")
	}
	ctx := context.Background()
	feed, err := s.db.GetFeedID(ctx, cmd.Args[0])
	if err != nil {
		return fmt.Errorf("You're not following %w", err)
	}
	err = s.db.UnfollowFeed(ctx, database.UnfollowFeedParams{
		UserID: user.ID,
		FeedID: feed.ID,
	})
	if err != nil {
		return fmt.Errorf("error unfollowing feed: %w", err)
	}
	fmt.Println("Feed unfollowed successfully")
	return nil
}

func scrapeFeeds(s *state) error {
	ctx := context.Background()
	feed, err := s.db.GetNextFeed(ctx)
	if err != nil {
		return fmt.Errorf("error getting next feed: %w", err)
	}
	content, err := fetchFeed(ctx, feed.Url)
	if err != nil {
		return fmt.Errorf("error fetching feed: %w", err)
	}
	uuid, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("error generating UUID for post: %w", err)
	}
	
	for _, item := range content.Channel.Item {
		desc := sql.NullString{String: item.Description, Valid: item.Description != ""}
		pubdate, err := time.Parse(time.RFC1123Z, item.PubDate)
		if err != nil {
			return fmt.Errorf("error parsing published date: %w", err)
		}
		s.db.CreatePost(ctx, database.CreatePostParams{
			ID: uuid,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Title: item.Title,
			Url: item.Link,
			Description: desc,
			PublishedAt: pubdate,
			FeedID: feed.ID,
		})
	}
	err = s.db.MarkFetched(context.Background(), feed.ID)
	if err != nil {
		return fmt.Errorf("error marking feed as fetched: %w", err)
	}
	return nil
}

func browse(s *state, cmd command, user database.User) error {
	var lim int32
	var err error
	if len(cmd.Args) < 1 {
		lim = 2
	} else {
		temp, err := strconv.Atoi(cmd.Args[0])
		if err != nil {
			return fmt.Errorf("invalid limit: %w", err)
		}
		lim = int32(temp)
	}
	ctx := context.Background()
	posts, err := s.db.GetPostsForUser(ctx, database.GetPostsForUserParams{
		UserID: user.ID, 
		Limit: lim})
	if err != nil {
		return fmt.Errorf("error getting posts: %w", err)
	}
	fmt.Println("Posts:")
	for _, post := range posts {
		fmt.Printf("- %s (%s) from feed %s\n", post.Title, post.Url, post.Description)
	}
	return nil
}


func main() {
	cfg, err := config.Read()
	if err != nil {
		fmt.Println("Error reading config:", err)
		return
	}
	db, err := sql.Open("postgres", cfg.Db_url)
	if err != nil {
		fmt.Println("Error opening database:", err)
		return
	}
	dbQueries := database.New(db)
	s := &state{db: dbQueries, config: &cfg}
	cmds := &commands{handlers: make(map[string]func(*state, command) error)}
	cmds.register("login", handlerLogin)
	cmds.register("register", handlerRegister)
	cmds.register("reset", reset)
	cmds.register("users", users)
	cmds.register("agg", agg)
	cmds.register("addfeed", middlewareLoggedIn(addfeed))
	cmds.register("feeds", feedsHandler)
	cmds.register("follow", middlewareLoggedIn(AddFeedFollow))
	cmds.register("following", middlewareLoggedIn(following))
	cmds.register("unfollow", middlewareLoggedIn(unfollow))
	cmds.register("browse", middlewareLoggedIn(browse))
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("No command provided")
		os.Exit(1)
		return
	}
	cmd := command{Name: args[0], Args: args[1:]}
	err = cmds.run(s, cmd)
	if err != nil {
		fmt.Printf("Command executed with error: %v\n", err)
		os.Exit(1)
	}
}