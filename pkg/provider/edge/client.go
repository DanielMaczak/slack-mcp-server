package edge

import (
	"context"
	"runtime/trace"

	"github.com/korotovsky/slack-mcp-server/pkg/limiter"
	"github.com/korotovsky/slack-mcp-server/pkg/provider/edge/fasttime"
	"github.com/rusq/slack"
)

// client.* API

type clientCountsForm struct {
	BaseRequest
	ThreadCountsByChannel bool `json:"thread_counts_by_channel"`
	OrgWideAware          bool `json:"org_wide_aware"`
	IncludeFileChannels   bool `json:"include_file_channels"`
	WebClientFields
}

type ClientCountsResponse struct {
	baseResponse
	Channels []ChannelSnapshot `json:"channels,omitempty"`
	MPIMs    []ChannelSnapshot `json:"mpims,omitempty"`
	IMs      []ChannelSnapshot `json:"ims,omitempty"`
}

type ChannelSnapshot struct {
	ID             string        `json:"id"`
	LastRead       fasttime.Time `json:"last_read"`
	Latest         fasttime.Time `json:"latest"`
	HistoryInvalid fasttime.Time `json:"history_invalid"`
	MentionCount   int           `json:"mention_count"`
	HasUnreads     bool          `json:"has_unreads"`
}

func (cl *Client) ClientCounts(ctx context.Context) (ClientCountsResponse, error) {
	ctx, task := trace.NewTask(ctx, "ClientCounts")
	defer task.End()

	form := clientCountsForm{
		BaseRequest:           BaseRequest{Token: cl.token},
		ThreadCountsByChannel: true,
		OrgWideAware:          true,
		IncludeFileChannels:   true,
		WebClientFields:       webclientReason("client-counts-api/fetchClientCounts"),
	}

	resp, err := cl.PostForm(ctx, "client.counts", values(form, true))
	if err != nil {
		return ClientCountsResponse{}, err
	}
	r := ClientCountsResponse{}
	if err := cl.ParseResponse(&r, resp); err != nil {
		return ClientCountsResponse{}, err
	}
	if err := r.validate("client.counts"); err != nil {
		return ClientCountsResponse{}, err
	}
	return r, nil
}

// subscriptions.* API

type subscriptionsThreadGetViewForm struct {
	BaseRequest
	CurrentTs    string `json:"current_ts"`
	Limit        int    `json:"limit"`
	OrgWideAware bool   `json:"org_wide_aware"`
	WebClientFields
}

type ThreadViewMessage struct {
	User     string `json:"user"`
	Ts       string `json:"ts"`
	Text     string `json:"text"`
	ThreadTs string `json:"thread_ts"`
	Channel  string `json:"channel"`
}

type ThreadView struct {
	RootMsg       ThreadViewMessage   `json:"root_msg"`
	UnreadReplies []ThreadViewMessage `json:"unread_replies"`
	LatestReplies []ThreadViewMessage `json:"latest_replies"`
}

type SubscriptionsThreadViewResponse struct {
	baseResponse
	Threads            []ThreadView `json:"threads"`
	TotalUnreadReplies int          `json:"total_unread_replies"`
	NewThreadsCount    int          `json:"new_threads_count"`
	HasMore            bool         `json:"has_more"`
	MaxTs              string       `json:"max_ts"`
}

// SubscriptionsThreadGetView returns the "Threads" view: subscribed threads with
// unread replies — the Activity feed that channel-level client.counts omits.
func (cl *Client) SubscriptionsThreadGetView(ctx context.Context, currentTs string, limit int) (SubscriptionsThreadViewResponse, error) {
	ctx, task := trace.NewTask(ctx, "SubscriptionsThreadGetView")
	defer task.End()

	form := subscriptionsThreadGetViewForm{
		BaseRequest:     BaseRequest{Token: cl.token},
		CurrentTs:       currentTs,
		Limit:           limit,
		OrgWideAware:    true,
		WebClientFields: webclientReason("fetch-threads-view-counts/fetchThreadsViewCounts"),
	}

	resp, err := cl.PostForm(ctx, "subscriptions.thread.getView", values(form, true))
	if err != nil {
		return SubscriptionsThreadViewResponse{}, err
	}
	r := SubscriptionsThreadViewResponse{}
	if err := cl.ParseResponse(&r, resp); err != nil {
		return SubscriptionsThreadViewResponse{}, err
	}
	if err := r.validate("subscriptions.thread.getView"); err != nil {
		return SubscriptionsThreadViewResponse{}, err
	}
	return r, nil
}

type clientDMsForm struct {
	BaseRequest
	Count          int    `json:"count"`
	IncludeClosed  bool   `json:"include_closed"`
	IncludeChannel bool   `json:"include_channel"`
	ExcludeBots    bool   `json:"exclude_bots"`
	Cursor         string `json:"cursor,omitempty"`
	WebClientFields
}

type clientDMsResponse struct {
	baseResponse
	IMs   []ClientDM `json:"ims,omitempty"`
	MPIMs []ClientDM `json:"mpims,omitempty"` //TODO
}

type ClientDM struct {
	ID string `json:"id"`
	// Message slack.Message `json:"message,omitempty"`
	Channel IM            `json:"channel,omitempty"`
	Latest  fasttime.Time `json:"latest,omitempty"` // i.e. "1710632873.037269"
}

type IM struct {
	ID               string         `json:"id"`
	Created          slack.JSONTime `json:"created"`
	IsFrozen         bool           `json:"is_frozen"`
	IsArchived       bool           `json:"is_archived"`
	IsIM             bool           `json:"is_im"`
	IsOrgShared      bool           `json:"is_org_shared"`
	ContextTeamID    string         `json:"context_team_id"`
	Updated          slack.JSONTime `json:"updated"`
	IsShared         bool           `json:"is_shared"`
	IsExtShared      bool           `json:"is_ext_shared"`
	User             string         `json:"user"`
	LastRead         fasttime.Time  `json:"last_read"`
	Latest           fasttime.Time  `json:"latest"`
	IsOpen           bool           `json:"is_open"`
	SharedTeamIds    []string       `json:"shared_team_ids"`
	ConnectedTeamIds []string       `json:"connected_team_ids"`
}

func (c IM) SlackChannel() slack.Channel {
	// Add Members array with just the User for IM channels
	var members []string
	if c.User != "" {
		members = []string{c.User}
	}

	return slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID:          c.ID,
				Created:     c.Created,
				IsIM:        c.IsIM,
				IsOrgShared: c.IsOrgShared,
				User:        c.User,
				LastRead:    c.LastRead.SlackString(),
			},
			IsArchived: c.IsArchived,
			Members:    members,
		},
	}

}

func (cl *Client) ClientDMs(ctx context.Context) ([]ClientDM, error) {
	form := clientDMsForm{
		BaseRequest:     BaseRequest{Token: cl.token},
		Count:           250,
		IncludeClosed:   true,
		IncludeChannel:  true,
		ExcludeBots:     false,
		Cursor:          "",
		WebClientFields: webclientReason("dms-tab-populate"),
	}
	lim := limiter.Tier2boost.Limiter()
	var IMs []ClientDM
	for {
		resp, err := cl.PostFormRaw(ctx, cl.webapiURL("client.dms"), values(form, true))
		if err != nil {
			return nil, err
		}
		r := clientDMsResponse{}
		if err := cl.ParseResponse(&r, resp); err != nil {
			return nil, err
		}
		IMs = append(IMs, r.IMs...)
		if r.ResponseMetadata.NextCursor == "" {
			break
		}
		form.Cursor = r.ResponseMetadata.NextCursor
		if err := lim.Wait(ctx); err != nil {
			return nil, err
		}
	}
	return IMs, nil
}
