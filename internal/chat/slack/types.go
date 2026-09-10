package slack

// SlashCommand is the URL-encoded payload sent by Slack for /crypto-alert.
type SlashCommand struct {
	Token       string `json:"token"`
	TeamID      string `json:"team_id"`
	APIAppID    string `json:"api_app_id"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	Command     string `json:"command"`
	Text        string `json:"text"`
	ResponseURL string `json:"response_url"`
	TriggerID   string `json:"trigger_id"`
}

// Interaction is the JSON payload sent by Slack for a Block Kit action.
type Interaction struct {
	Type        string        `json:"type"`
	APIAppID    string        `json:"api_app_id"`
	Team        SlackTeam     `json:"team"`
	User        SlackUser     `json:"user"`
	Channel     SlackChannel  `json:"channel"`
	Actions     []BlockAction `json:"actions"`
	ResponseURL string        `json:"response_url"`
	TriggerID   string        `json:"trigger_id"`
}

// SlackTeam identifies the workspace that owns the Slack app.
type SlackTeam struct {
	ID string `json:"id"`
}

// SlackUser identifies the actor who invoked a command or interaction.
type SlackUser struct {
	ID string `json:"id"`
}

// SlackChannel identifies the channel in which the interaction occurred.
type SlackChannel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// BlockAction is the selected Block Kit control.
type BlockAction struct {
	ActionID string `json:"action_id"`
	Value    string `json:"value,omitempty"`
}

// ChannelInfo is the subset returned by conversations.info that is needed for creator authorization.
type ChannelInfo struct {
	ID      string `json:"id"`
	Creator string `json:"creator"`
}

type apiResponse[T any] struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Channel T      `json:"channel,omitempty"`
}

// Response is an ephemeral or in-channel response sent to Slack.
type Response struct {
	ResponseType string  `json:"response_type,omitempty"`
	Text         string  `json:"text,omitempty"`
	Blocks       []Block `json:"blocks,omitempty"`
}

// Block is a Block Kit layout element.
type Block struct {
	Type      string         `json:"type"`
	Text      *TextObject    `json:"text,omitempty"`
	Elements  []BlockElement `json:"elements,omitempty"`
	Accessory *BlockElement  `json:"accessory,omitempty"`
}

// TextObject is plain or markdown text used by Block Kit.
type TextObject struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// BlockElement is a button or static select control.
type BlockElement struct {
	Type     string      `json:"type"`
	ActionID string      `json:"action_id,omitempty"`
	Text     *TextObject `json:"text,omitempty"`
	Value    string      `json:"value,omitempty"`
	Options  []Option    `json:"options,omitempty"`
	Option   *Option     `json:"selected_option,omitempty"`
}

// Option is a selectable Block Kit value.
type Option struct {
	Text  TextObject `json:"text"`
	Value string     `json:"value"`
}
