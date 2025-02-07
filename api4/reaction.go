// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api4

import (
	"encoding/json"
	"net/http"

	"github.com/mattermost/mattermost-server/v6/model"
	"github.com/mattermost/mattermost-server/v6/shared/mlog"
)

func (api *API) InitReaction() {
	api.BaseRoutes.Reactions.Handle("", api.ApiSessionRequired(saveReaction)).Methods("POST")
	api.BaseRoutes.Post.Handle("/reactions", api.ApiSessionRequired(getReactions)).Methods("GET")
	api.BaseRoutes.ReactionByNameForPostForUser.Handle("", api.ApiSessionRequired(deleteReaction)).Methods("DELETE")
	api.BaseRoutes.Posts.Handle("/ids/reactions", api.ApiSessionRequired(getBulkReactions)).Methods("POST")
}

func saveReaction(c *Context, w http.ResponseWriter, r *http.Request) {
	reaction := model.ReactionFromJson(r.Body)
	if reaction == nil {
		c.SetInvalidParam("reaction")
		return
	}

	if !model.IsValidId(reaction.UserId) || !model.IsValidId(reaction.PostId) || reaction.EmojiName == "" || len(reaction.EmojiName) > model.EmojiNameMaxLength {
		c.Err = model.NewAppError("saveReaction", "api.reaction.save_reaction.invalid.app_error", nil, "", http.StatusBadRequest)
		return
	}

	if reaction.UserId != c.AppContext.Session().UserId {
		c.Err = model.NewAppError("saveReaction", "api.reaction.save_reaction.user_id.app_error", nil, "", http.StatusForbidden)
		return
	}

	if !c.App.SessionHasPermissionToChannelByPost(*c.AppContext.Session(), reaction.PostId, model.PermissionAddReaction) {
		c.SetPermissionError(model.PermissionAddReaction)
		return
	}

	reaction, err := c.App.SaveReactionForPost(c.AppContext, reaction)
	if err != nil {
		c.Err = err
		return
	}

	if err := json.NewEncoder(w).Encode(reaction); err != nil {
		mlog.Warn("Error while writing response", mlog.Err(err))
	}
}

func getReactions(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequirePostId()
	if c.Err != nil {
		return
	}

	if !c.App.SessionHasPermissionToChannelByPost(*c.AppContext.Session(), c.Params.PostId, model.PermissionReadChannel) {
		c.SetPermissionError(model.PermissionReadChannel)
		return
	}

	reactions, err := c.App.GetReactionsForPost(c.Params.PostId)
	if err != nil {
		c.Err = err
		return
	}

	w.Write([]byte(model.ReactionsToJson(reactions)))
}

func deleteReaction(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireUserId()
	if c.Err != nil {
		return
	}

	c.RequirePostId()
	if c.Err != nil {
		return
	}

	c.RequireEmojiName()
	if c.Err != nil {
		return
	}

	if !c.App.SessionHasPermissionToChannelByPost(*c.AppContext.Session(), c.Params.PostId, model.PermissionRemoveReaction) {
		c.SetPermissionError(model.PermissionRemoveReaction)
		return
	}

	if c.Params.UserId != c.AppContext.Session().UserId && !c.App.SessionHasPermissionTo(*c.AppContext.Session(), model.PermissionRemoveOthersReactions) {
		c.SetPermissionError(model.PermissionRemoveOthersReactions)
		return
	}

	reaction := &model.Reaction{
		UserId:    c.Params.UserId,
		PostId:    c.Params.PostId,
		EmojiName: c.Params.EmojiName,
	}

	err := c.App.DeleteReactionForPost(c.AppContext, reaction)
	if err != nil {
		c.Err = err
		return
	}

	ReturnStatusOK(w)
}

func getBulkReactions(c *Context, w http.ResponseWriter, r *http.Request) {
	postIds := model.ArrayFromJson(r.Body)
	for _, postId := range postIds {
		if !c.App.SessionHasPermissionToChannelByPost(*c.AppContext.Session(), postId, model.PermissionReadChannel) {
			c.SetPermissionError(model.PermissionReadChannel)
			return
		}
	}
	reactions, err := c.App.GetBulkReactionsForPosts(postIds)
	if err != nil {
		c.Err = err
		return
	}

	w.Write([]byte(model.MapPostIdToReactionsToJson(reactions)))
}
