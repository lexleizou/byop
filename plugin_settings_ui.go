package main

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

type pluginSettingForm struct {
	pluginID    string
	settingID   string
	inputs      map[string]*string
	booleans    map[string]*bool
	selections  map[string]string
	warning     string
	status      string
	statusError bool
	saving      bool
}

func (s *appState) syncPluginSettingForms() {
	index := make(map[string]struct{})
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		settings, err := s.plugins.settingsFor(entry)
		if err != nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Settings {
			key := entry.ID + "\x00" + contribution.ID
			index[key] = struct{}{}
			form, exists := s.pluginSettingForms[key]
			if !exists {
				form = &pluginSettingForm{
					pluginID:  entry.ID,
					settingID: contribution.ID,
				}
				s.pluginSettingForms[key] = form
			}
			form.inputs = make(map[string]*string)
			form.booleans = make(map[string]*bool)
			form.selections = make(map[string]string)
			form.warning = s.plugins.settingsWarning(entry.ID)
			form.status = ""
			form.statusError = false
			form.saving = false
			for _, field := range contribution.Fields {
				value := settings[field.Key]
				switch field.Type {
				case "string":
					text := settingString(value)
					input := text
					form.inputs[field.Key] = &input
				case "number":
					text := settingNumberString(value)
					input := text
					form.inputs[field.Key] = &input
				case "boolean":
					boolean, _ := value.(bool)
					input := boolean
					form.booleans[field.Key] = &input
				case "select":
					form.selections[field.Key] = settingString(value)
				}
			}
		}
	}
	for key := range s.pluginSettingForms {
		if _, ok := index[key]; !ok {
			delete(s.pluginSettingForms, key)
		}
	}
}

func (s *appState) pluginSettingsSection(c *ui.Context, p palette) {
	hasSettings := false
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Settings {
			hasSettings = true
			s.pluginSettingCard(c, p, entry, contribution)
		}
	}
	if hasSettings {
		return
	}
	ui.Column(c).FillWidth().Gap(8).Padding(16).Radius(12).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Text(c, "No plugin settings").FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Text(c, "Registered plugins have not declared typed setting fields.").FontSize(fontSmall).
			TextColor(p.muted)
	})
}

func (s *appState) pluginSettingCard(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	contribution pluginSettingContribution,
) {
	key := entry.ID + "\x00" + contribution.ID
	form := s.pluginSettingForms[key]
	if form == nil {
		return
	}
	description := fallback(contribution.Description,
		"Settings are stored locally by BYOP and passed to the plugin when it starts.")
	settingsCard(c, p, contribution.Title, description, func() {
		ui.Text(c, fallback(entry.Name, entry.ID)).FontSize(fontCaption).
			FontWeight(650).TextColor(p.accent)
		if form.warning != "" {
			ui.Text(c, form.warning).FontSize(fontCaption).LineHeight(1.4).
				TextColor(p.warning)
		}
		if len(contribution.Fields) == 0 {
			ui.Text(c, "This setting contribution does not declare editable fields.").
				FontSize(fontSmall).TextColor(p.muted)
		}
		for _, field := range contribution.Fields {
			s.pluginSettingField(c, p, entry, form, field)
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			label := "Save"
			if form.saving {
				label = "Saving…"
			}
			secondaryAction(c, p,
				"plugin-settings-save-"+entry.ID+"-"+contribution.ID,
				label,
				func() {
					if !form.saving {
						s.savePluginSettings(entry, contribution, form)
					}
				},
			)
			if form.status != "" {
				color := p.success
				if form.statusError {
					color = p.danger
				}
				ui.Text(c, form.status).FontSize(fontCaption).TextColor(color)
			}
		})
	})
}

func (s *appState) pluginSettingField(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	form *pluginSettingForm,
	field pluginSettingField,
) {
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, field.Label).FontSize(fontSmall).FontWeight(600).TextColor(p.text)
		if field.Description != "" {
			ui.Text(c, field.Description).FontSize(fontCaption).TextColor(p.muted)
		}
		switch field.Type {
		case "string", "number":
			input := form.inputs[field.Key]
			ui.Row(c).Height(36).Padding(0, 10).Radius(8).Border(1, p.border).
				Background(p.background).AlignItems(ui.Center).Children(func() {
				ui.TextInputBase(c.Key("plugin-setting-"+entry.ID+"-"+field.Key), input).
					Grow(1).Placeholder(field.Placeholder).FontSize(fontBody).
					TextColor(p.text).Background(ui.Color{}).Border(0, ui.Color{}).
					Padding(0).Label(field.Label)
			})
			if field.Type == "number" && input != nil && strings.TrimSpace(*input) != "" {
				if _, err := parsePluginSettingNumber(*input); err != nil {
					ui.Text(c, "Enter a valid number.").FontSize(fontCaption).TextColor(p.danger)
				}
			}
		case "boolean":
			ui.Row(c).FillWidth().Padding(8, 10).Radius(7).Background(p.background).
				AlignItems(ui.Center).Children(func() {
				ui.Text(c, field.Label).FontSize(fontCaption).TextColor(p.muted).Grow(1)
				if input := form.booleans[field.Key]; input != nil {
					ui.Switch(c, input).Label(field.Label)
				}
			})
		case "select":
			ui.Row(c).Wrap().Gap(6).Children(func() {
				for _, option := range field.Options {
					option := option
					selected := form.selections[field.Key] == option
					textButton(c, p, "plugin-setting-"+entry.ID+"-"+field.Key+"-"+option,
						option, selected, func() {
							form.selections[field.Key] = option
						})
				}
			})
		}
	})
}

func (s *appState) savePluginSettings(
	entry pluginRegistryEntry,
	contribution pluginSettingContribution,
	form *pluginSettingForm,
) {
	values := make(map[string]any, len(contribution.Fields))
	for _, field := range contribution.Fields {
		switch field.Type {
		case "string":
			if input := form.inputs[field.Key]; input != nil {
				values[field.Key] = *input
			}
		case "number":
			input := form.inputs[field.Key]
			if input == nil {
				form.status = "Enter a valid number."
				form.statusError = true
				return
			}
			number, err := parsePluginSettingNumber(*input)
			if err != nil {
				form.status = "Enter a valid number."
				form.statusError = true
				return
			}
			values[field.Key] = number
		case "boolean":
			if input := form.booleans[field.Key]; input != nil {
				values[field.Key] = *input
			}
		case "select":
			values[field.Key] = form.selections[field.Key]
		}
	}
	form.saving = true
	form.status = "Saving…"
	form.statusError = false
	go func() {
		err := s.plugins.savePluginSettings(entry, values)
		warning := s.plugins.settingsWarning(entry.ID)
		s.postUI(func() {
			form.saving = false
			for _, candidate := range s.pluginSettingForms {
				if candidate.pluginID == entry.ID {
					candidate.warning = warning
				}
			}
			if err != nil {
				form.status = summarizeLine(err.Error(), 220)
				form.statusError = true
				return
			}
			form.status = "Saved"
			form.statusError = false
		})
	}()
}

func parsePluginSettingNumber(value string) (float64, error) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, strconv.ErrSyntax
	}
	return number, nil
}

func settingString(value any) string {
	text, _ := value.(string)
	return text
}

func settingNumberString(value any) string {
	switch number := value.(type) {
	case float64:
		return strconv.FormatFloat(number, 'f', -1, 64)
	case jsonNumber:
		return number.String()
	default:
		return "0"
	}
}

type jsonNumber interface {
	String() string
}
