package telegrambot

import (
	"sort"
	"strings"

	"github.com/go-telegram/bot/models"
)

// commandGroup is a section of the /help listing.
type commandGroup string

const (
	groupGeneral    commandGroup = "General"
	groupAuth       commandGroup = "Authentication"
	groupGeneration commandGroup = "Generation and options"
	groupGallery    commandGroup = "Gallery"
	groupAdmin      commandGroup = "Admin"
)

// groupOrder fixes the order sections appear in /help.
var groupOrder = []commandGroup{groupGeneral, groupAuth, groupGeneration, groupGallery, groupAdmin}

// commandSpec documents one slash command. The catalog below is the single
// source of truth for both the Telegram "/" menu (setMyCommands) and /help.
type commandSpec struct {
	name  string
	args  string // argument syntax shown in /help, "" for none
	desc  string // one line; Telegram caps menu descriptions at 256 chars
	group commandGroup
	// inMenu publishes the command to Telegram's "/" list. Only the handful
	// a user might reach for when the buttons are not enough is published:
	// a long "/" list is exactly the memorization the menu exists to avoid.
	// Everything else still works when typed and is still listed by /help.
	inMenu bool
}

// commandCatalog lists every command a user can type.
var commandCatalog = []commandSpec{
	{name: "start", desc: "Show what the bot does and open the menu", group: groupGeneral, inMenu: true},
	{name: "menu", desc: "Open the menu", group: groupGeneral, inMenu: true},
	{name: "help", desc: "List every command", group: groupGeneral, inMenu: true},

	{name: "login", args: "<username> <password>", desc: "Authenticate this chat", group: groupAuth, inMenu: true},
	{name: "logout", desc: "End a temporary login and clear selected options", group: groupAuth},

	{name: "new", desc: "Start a new image", group: groupGeneration},
	{name: "generate", args: "<prompt>", desc: "Generate images from a prompt", group: groupGeneration},
	{name: "models", desc: "List the known models and pick one", group: groupGeneration},
	{name: "model", args: "[id]", desc: "Show or set the active model", group: groupGeneration},
	{name: "aspect", args: "[ratio]", desc: "Show or set the aspect ratio", group: groupGeneration},
	{name: "size", args: "[size]", desc: "Show or set the image size", group: groupGeneration},
	{name: "count", args: "[n]", desc: "Show or set how many images to generate (1-8)", group: groupGeneration},
	{name: "settings", desc: "Model and generation options", group: groupGeneration, inMenu: true},

	{name: "gallery", desc: "Your images", group: groupGallery, inMenu: true},
	{name: "browse", desc: "Page through your images one by one", group: groupGallery},
	{name: "delete", args: "<id>", desc: "Delete one of your images", group: groupGallery},
	{name: "clear", desc: "Delete every image you own", group: groupGallery},

	{name: "admin", args: "<password>", desc: "Authorize this chat as admin", group: groupAdmin},
	{name: "adduser", args: "<username> <password> [telegram_id...]", desc: "Create a user", group: groupAdmin},
	{name: "addtelegramid", args: "<username> <telegram_id>", desc: "Link a Telegram ID to a user", group: groupAdmin},
	{name: "listusers", desc: "List users and their linked Telegram IDs", group: groupAdmin},
}

// menuCommands is the short list published to Telegram's "/" menu, in catalog
// order. Admin commands are never published; nor is anything the menu already
// covers with a button.
func menuCommands() []models.BotCommand {
	out := make([]models.BotCommand, 0, len(commandCatalog))
	for _, c := range commandCatalog {
		if !c.inMenu || c.group == groupAdmin {
			continue
		}
		out = append(out, models.BotCommand{Command: c.name, Description: c.desc})
	}
	return out
}

// usage returns the "/name <args>" form used in help and usage errors.
func (c commandSpec) usage() string {
	if c.args == "" {
		return "/" + c.name
	}
	return "/" + c.name + " " + c.args
}

// helpPages renders the whole catalog, grouped, as messages short enough to
// send (the catalog is far below the limit today; paginate keeps it safe).
func helpPages() []string {
	byGroup := map[commandGroup][]commandSpec{}
	for _, c := range commandCatalog {
		byGroup[c.group] = append(byGroup[c.group], c)
	}

	lines := []string{
		"Imagen bot",
		"",
		"You do not have to remember any of this: send a prompt as a plain",
		"message, and use the buttons under the input field for the rest.",
		"",
		"Buttons",
		"  " + btnGenerate + " — how to write a prompt, and what it will generate with",
		"  " + btnGallery + " — your images: a list, a one-at-a-time browser, delete",
		"  " + btnSettings + " — model, aspect ratio, image size, images per prompt",
		"  " + btnHelp + " — this text",
		"",
		"Commands",
		"",
	}
	for _, g := range groupOrder {
		cmds := byGroup[g]
		if len(cmds) == 0 {
			continue
		}
		header := string(g)
		if g == groupAdmin {
			header += " (needs /admin <password> first)"
		}
		lines = append(lines, header)
		for _, c := range cmds {
			lines = append(lines, "  "+c.usage()+" — "+c.desc)
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		"Shortcuts",
		"  Send plain text and it is used as a prompt.",
		"  Send a photo (or reply to one) with a caption to use it as a reference image.",
		"  Under every image: 🔄 Again, 📎 Original (uncompressed), 🗑 Delete.",
	)
	return paginate(lines, maxMessageLen)
}

// unknownCommandHint suggests the closest catalog command for an unrecognized
// one, or "" when nothing is close enough.
func unknownCommandHint(name string) string {
	name = strings.ToLower(name)
	var best string
	bestScore := 0
	for _, c := range commandCatalog {
		score := commonPrefixLen(name, c.name)
		if score > bestScore {
			bestScore, best = score, c.name
		}
	}
	if bestScore < 3 {
		return ""
	}
	return best
}

func commonPrefixLen(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// knownCommand reports whether name is a documented command.
func knownCommand(name string) bool {
	i := sort.Search(len(sortedCommandNames), func(i int) bool { return sortedCommandNames[i] >= name })
	return i < len(sortedCommandNames) && sortedCommandNames[i] == name
}

var sortedCommandNames = func() []string {
	names := make([]string, 0, len(commandCatalog))
	for _, c := range commandCatalog {
		names = append(names, c.name)
	}
	sort.Strings(names)
	return names
}()
