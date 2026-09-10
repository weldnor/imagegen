package telegrambot

import (
	"strconv"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/openrouter"
)

// AspectRatios and ImageSizes are the presets the buttons offer: every value
// the upstream API accepts, so /aspect and /size take the same set.
var (
	AspectRatios = openrouter.AspectRatios
	ImageSizes   = openrouter.ImageSizes
)

func button(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

// mark prefixes the label of the currently selected option.
func mark(label string, selected bool) string {
	if selected {
		return "✅ " + label
	}
	return label
}

// chunk lays buttons out in rows of at most perRow.
func chunk(btns []models.InlineKeyboardButton, perRow int) [][]models.InlineKeyboardButton {
	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(btns); i += perRow {
		end := i + perRow
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, btns[i:end])
	}
	return rows
}

// navRow is the footer of every screen below the root: back to the screen it
// was opened from, and a way out of the menu entirely.
func navRow(back string) []models.InlineKeyboardButton {
	return []models.InlineKeyboardButton{
		button("⬅️ Back", back),
		button("✖️ Close", cbMenuClose),
	}
}

// homeKeyboard is the persistent keyboard under the input field. It is the
// only thing a user has to know about: every command worth typing is one of
// these four taps, and route() turns each label back into its command.
func homeKeyboard() replyKeyboard {
	return replyKeyboard{
		Keyboard: [][]models.KeyboardButton{
			{{Text: btnGenerate}, {Text: btnGallery}},
			{{Text: btnSettings}, {Text: btnHelp}},
		},
		ResizeKeyboard:        true,
		IsPersistent:          true,
		InputFieldPlaceholder: "Describe the image you want…",
	}
}

// mainMenuKeyboard is the root of the inline menu: three destinations, no
// settings clutter — those live one level down, behind ⚙️ Settings.
func mainMenuKeyboard() keyboard {
	return keyboard{InlineKeyboard: [][]models.InlineKeyboardButton{
		{button("🖼 Gallery", cbGalleryList), button("⚙️ Settings", cbMenuSettings)},
		{button("❓ Help", cbMenuHelp), button("✖️ Close", cbMenuClose)},
	}}
}

// settingsKeyboard is the settings submenu. Each button carries the value in
// force, so the screen reads as a list of settings rather than of verbs, and
// options the active model does not support are left out instead of shown as
// dead ends.
func settingsKeyboard(s activeSettings) keyboard {
	rows := [][]models.InlineKeyboardButton{
		{button("🎨 Model · "+s.cfg.Name, cbMenuModel)},
		{button("🔢 Images per prompt · "+strconv.Itoa(s.count), cbMenuCount)},
	}
	if s.cfg.SupportsAspectRatio {
		rows = append(rows, []models.InlineKeyboardButton{
			button("📐 Aspect ratio · "+shortValue(s.aspect), cbMenuAspect),
		})
	}
	if s.cfg.SupportsImageSize {
		rows = append(rows, []models.InlineKeyboardButton{
			button("🖼 Image size · "+shortValue(s.size), cbMenuSize),
		})
	}
	return keyboard{InlineKeyboard: append(rows,
		[]models.InlineKeyboardButton{button("♻️ Reset to defaults", cbMenuReset)},
		navRow(cbMenuMain),
	)}
}

// shortValue labels a button for an option the chat has not set.
func shortValue(v string) string {
	if v == "" {
		return "auto"
	}
	return v
}

// modelKeyboard lists every known model, the active one marked.
func modelKeyboard(active string) keyboard {
	ids := openrouter.KnownModels()
	rows := make([][]models.InlineKeyboardButton, 0, len(ids)+1)
	for _, id := range ids {
		rows = append(rows, []models.InlineKeyboardButton{
			button(mark(openrouter.Models[id].Name, id == active), cbPrefixModel+":"+id),
		})
	}
	return keyboard{InlineKeyboard: append(rows, navRow(cbMenuSettings))}
}

// aspectOrder puts the ratios people actually ask for in the first rows,
// rather than the numeric order openrouter lists them in. It is a display
// order only: orderedAspectRatios appends anything missing from it, so the
// picker still offers every ratio the API accepts.
var aspectOrder = []string{
	"1:1", "16:9", "9:16", "4:3",
	"3:4", "3:2", "2:3", "21:9",
	"4:5", "5:4",
}

// orderedAspectRatios is AspectRatios in button order.
func orderedAspectRatios() []string {
	out := make([]string, 0, len(AspectRatios))
	seen := make(map[string]bool, len(AspectRatios))
	for _, want := range aspectOrder {
		for _, r := range AspectRatios {
			if r == want && !seen[r] {
				out, seen[r] = append(out, r), true
			}
		}
	}
	for _, r := range AspectRatios {
		if !seen[r] {
			out = append(out, r)
		}
	}
	return out
}

func aspectKeyboard(active string) keyboard {
	btns := make([]models.InlineKeyboardButton, 0, len(AspectRatios)+1)
	btns = append(btns, button(mark("auto", active == ""), cbPrefixAspect+":auto"))
	for _, r := range orderedAspectRatios() {
		btns = append(btns, button(mark(r, r == active), cbPrefixAspect+":"+r))
	}
	return keyboard{InlineKeyboard: append(chunk(btns, 4), navRow(cbMenuSettings))}
}

func sizeKeyboard(active string) keyboard {
	btns := make([]models.InlineKeyboardButton, 0, len(ImageSizes)+1)
	btns = append(btns, button(mark("auto", active == ""), cbPrefixSize+":auto"))
	for _, s := range ImageSizes {
		btns = append(btns, button(mark(s, s == active), cbPrefixSize+":"+s))
	}
	return keyboard{InlineKeyboard: append(chunk(btns, 3), navRow(cbMenuSettings))}
}

func countKeyboard(active int) keyboard {
	btns := make([]models.InlineKeyboardButton, 0, maxCount)
	for n := 1; n <= maxCount; n++ {
		s := strconv.Itoa(n)
		btns = append(btns, button(mark(s, n == active), cbPrefixCount+":"+s))
	}
	return keyboard{InlineKeyboard: append(chunk(btns, 4), navRow(cbMenuSettings))}
}

// galleryKeyboard accompanies the gallery listing.
func galleryKeyboard(empty bool) keyboard {
	if empty {
		return keyboard{InlineKeyboard: [][]models.InlineKeyboardButton{navRow(cbMenuMain)}}
	}
	return keyboard{InlineKeyboard: [][]models.InlineKeyboardButton{
		{button("🖼 Browse images", cbGalleryBrowse+":0")},
		{button("🗑 Delete all", cbGalleryClear)},
		navRow(cbMenuMain),
	}}
}

// browseKeyboard is attached to one image of the gallery browser: navigation,
// per-image actions, and a way back to the listing.
func browseKeyboard(imageID string, index, total int) keyboard {
	prev, next := index-1, index+1
	if prev < 0 {
		prev = total - 1
	}
	if next >= total {
		next = 0
	}
	nav := []models.InlineKeyboardButton{
		button("◀️", cbGalleryBrowse+":"+strconv.Itoa(prev)),
		button(strconv.Itoa(index+1)+"/"+strconv.Itoa(total), cbNoop),
		button("▶️", cbGalleryBrowse+":"+strconv.Itoa(next)),
	}
	rows := [][]models.InlineKeyboardButton{}
	if total > 1 {
		rows = append(rows, nav)
	}
	rows = append(rows,
		imageActionRow(imageID),
		[]models.InlineKeyboardButton{button("🖼 Gallery", cbGalleryList), button("✖️ Close", cbMenuClose)},
	)
	return keyboard{InlineKeyboard: rows}
}

// imageActionRow are the buttons under a single image. "Original" resends the
// image as an uncompressed file, since Telegram re-encodes anything sent as a
// photo.
func imageActionRow(imageID string) []models.InlineKeyboardButton {
	return []models.InlineKeyboardButton{
		button("🔄 Again", cbPrefixImage+":"+cbImageRegen+":"+imageID),
		button("📎 Original", cbPrefixImage+":"+cbImageOriginal+":"+imageID),
		button("🗑 Delete", cbPrefixImage+":"+cbImageDelete+":"+imageID),
	}
}

// generatedImageKeyboard is attached to every freshly generated image: the
// per-image actions, plus a way into the menu. The menu button carries the
// same callback data as the root menu screen; cbMenu notices the press came
// from a photo (which has no text to edit in place) and sends the menu as a
// fresh message instead.
func generatedImageKeyboard(imageID string) keyboard {
	return keyboard{InlineKeyboard: [][]models.InlineKeyboardButton{
		imageActionRow(imageID),
		{button("☰ Menu", cbMenuMain)},
	}}
}

// confirmClearKeyboard guards the "delete all" button; the /clear command
// itself stays a one-shot, deliberate action.
func confirmClearKeyboard() keyboard {
	return keyboard{InlineKeyboard: [][]models.InlineKeyboardButton{{
		button("✅ Yes, delete all", cbGalleryClearYes),
		button("❌ Cancel", cbGalleryList),
	}}}
}

// backOnlyKeyboard is the keyboard for a screen with nothing to pick.
func backOnlyKeyboard() keyboard {
	return keyboard{InlineKeyboard: [][]models.InlineKeyboardButton{navRow(cbMenuMain)}}
}
