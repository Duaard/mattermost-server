// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api4

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/mattermost/mattermost-server/v6/audit"
	"github.com/mattermost/mattermost-server/v6/model"
	"github.com/mattermost/mattermost-server/v6/shared/mlog"
)

func (api *API) InitCommand() {
	api.BaseRoutes.Commands.Handle("", api.ApiSessionRequired(createCommand)).Methods("POST")
	api.BaseRoutes.Commands.Handle("", api.ApiSessionRequired(listCommands)).Methods("GET")
	api.BaseRoutes.Commands.Handle("/execute", api.ApiSessionRequired(executeCommand)).Methods("POST")

	api.BaseRoutes.Command.Handle("", api.ApiSessionRequired(getCommand)).Methods("GET")
	api.BaseRoutes.Command.Handle("", api.ApiSessionRequired(updateCommand)).Methods("PUT")
	api.BaseRoutes.Command.Handle("/move", api.ApiSessionRequired(moveCommand)).Methods("PUT")
	api.BaseRoutes.Command.Handle("", api.ApiSessionRequired(deleteCommand)).Methods("DELETE")

	api.BaseRoutes.Team.Handle("/commands/autocomplete", api.ApiSessionRequired(listAutocompleteCommands)).Methods("GET")
	api.BaseRoutes.Team.Handle("/commands/autocomplete_suggestions", api.ApiSessionRequired(listCommandAutocompleteSuggestions)).Methods("GET")
	api.BaseRoutes.Command.Handle("/regen_token", api.ApiSessionRequired(regenCommandToken)).Methods("PUT")
}

func createCommand(c *Context, w http.ResponseWriter, r *http.Request) {
	cmd := model.CommandFromJson(r.Body)
	if cmd == nil {
		c.SetInvalidParam("command")
		return
	}

	auditRec := c.MakeAuditRecord("createCommand", audit.Fail)
	defer c.LogAuditRec(auditRec)
	c.LogAudit("attempt")

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageSlashCommands) {
		c.SetPermissionError(model.PermissionManageSlashCommands)
		return
	}

	cmd.CreatorId = c.AppContext.Session().UserId

	rcmd, err := c.App.CreateCommand(cmd)
	if err != nil {
		c.Err = err
		return
	}

	auditRec.Success()
	c.LogAudit("success")
	auditRec.AddMeta("command", rcmd)

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(rcmd); err != nil {
		mlog.Warn("Error while writing response", mlog.Err(err))
	}
}

func updateCommand(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireCommandId()
	if c.Err != nil {
		return
	}

	cmd := model.CommandFromJson(r.Body)
	if cmd == nil || cmd.Id != c.Params.CommandId {
		c.SetInvalidParam("command")
		return
	}

	auditRec := c.MakeAuditRecord("updateCommand", audit.Fail)
	defer c.LogAuditRec(auditRec)
	c.LogAudit("attempt")

	oldCmd, err := c.App.GetCommand(c.Params.CommandId)
	if err != nil {
		auditRec.AddMeta("command_id", c.Params.CommandId)
		c.SetCommandNotFoundError()
		return
	}
	auditRec.AddMeta("command", oldCmd)

	if cmd.TeamId != oldCmd.TeamId {
		c.Err = model.NewAppError("updateCommand", "api.command.team_mismatch.app_error", nil, "user_id="+c.AppContext.Session().UserId, http.StatusBadRequest)
		return
	}

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), oldCmd.TeamId, model.PermissionManageSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		// here we return Not_found instead of a permissions error so we don't leak the existence of
		// a command to someone without permissions for the team it belongs to.
		c.SetCommandNotFoundError()
		return
	}

	if c.AppContext.Session().UserId != oldCmd.CreatorId && !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), oldCmd.TeamId, model.PermissionManageOthersSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		c.SetPermissionError(model.PermissionManageOthersSlashCommands)
		return
	}

	rcmd, err := c.App.UpdateCommand(oldCmd, cmd)
	if err != nil {
		c.Err = err
		return
	}

	auditRec.Success()
	c.LogAudit("success")

	if err := json.NewEncoder(w).Encode(rcmd); err != nil {
		mlog.Warn("Error while writing response", mlog.Err(err))
	}
}

func moveCommand(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireCommandId()
	if c.Err != nil {
		return
	}

	cmr, err := model.CommandMoveRequestFromJson(r.Body)
	if err != nil {
		c.SetInvalidParam("team_id")
		return
	}

	auditRec := c.MakeAuditRecord("moveCommand", audit.Fail)
	defer c.LogAuditRec(auditRec)
	c.LogAudit("attempt")

	newTeam, appErr := c.App.GetTeam(cmr.TeamId)
	if appErr != nil {
		c.Err = appErr
		return
	}
	auditRec.AddMeta("team", newTeam)

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), newTeam.Id, model.PermissionManageSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		c.SetPermissionError(model.PermissionManageSlashCommands)
		return
	}

	cmd, appErr := c.App.GetCommand(c.Params.CommandId)
	if appErr != nil {
		c.SetCommandNotFoundError()
		return
	}
	auditRec.AddMeta("command", cmd)

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		// here we return Not_found instead of a permissions error so we don't leak the existence of
		// a command to someone without permissions for the team it belongs to.
		c.SetCommandNotFoundError()
		return
	}

	if appErr = c.App.MoveCommand(newTeam, cmd); appErr != nil {
		c.Err = appErr
		return
	}

	auditRec.Success()
	c.LogAudit("success")

	ReturnStatusOK(w)
}

func deleteCommand(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireCommandId()
	if c.Err != nil {
		return
	}

	auditRec := c.MakeAuditRecord("deleteCommand", audit.Fail)
	defer c.LogAuditRec(auditRec)
	c.LogAudit("attempt")

	cmd, err := c.App.GetCommand(c.Params.CommandId)
	if err != nil {
		c.SetCommandNotFoundError()
		return
	}
	auditRec.AddMeta("command", cmd)

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		// here we return Not_found instead of a permissions error so we don't leak the existence of
		// a command to someone without permissions for the team it belongs to.
		c.SetCommandNotFoundError()
		return
	}

	if c.AppContext.Session().UserId != cmd.CreatorId && !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageOthersSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		c.SetPermissionError(model.PermissionManageOthersSlashCommands)
		return
	}

	err = c.App.DeleteCommand(cmd.Id)
	if err != nil {
		c.Err = err
		return
	}

	auditRec.Success()
	c.LogAudit("success")

	ReturnStatusOK(w)
}

func listCommands(c *Context, w http.ResponseWriter, r *http.Request) {
	customOnly, _ := strconv.ParseBool(r.URL.Query().Get("custom_only"))

	teamId := r.URL.Query().Get("team_id")
	if teamId == "" {
		c.SetInvalidParam("team_id")
		return
	}

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), teamId, model.PermissionViewTeam) {
		c.SetPermissionError(model.PermissionViewTeam)
		return
	}

	var commands []*model.Command
	var err *model.AppError
	if customOnly {
		if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), teamId, model.PermissionManageSlashCommands) {
			c.SetPermissionError(model.PermissionManageSlashCommands)
			return
		}
		commands, err = c.App.ListTeamCommands(teamId)
		if err != nil {
			c.Err = err
			return
		}
	} else {
		//User with no permission should see only system commands
		if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), teamId, model.PermissionManageSlashCommands) {
			commands, err = c.App.ListAutocompleteCommands(teamId, c.AppContext.T)
			if err != nil {
				c.Err = err
				return
			}
		} else {
			commands, err = c.App.ListAllCommands(teamId, c.AppContext.T)
			if err != nil {
				c.Err = err
				return
			}
		}
	}

	if err := json.NewEncoder(w).Encode(commands); err != nil {
		mlog.Warn("Error writing response", mlog.Err(err))
	}
}

func getCommand(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireCommandId()
	if c.Err != nil {
		return
	}

	cmd, err := c.App.GetCommand(c.Params.CommandId)
	if err != nil {
		c.SetCommandNotFoundError()
		return
	}

	// check for permissions to view this command; must have perms to view team and
	// PERMISSION_MANAGE_SLASH_COMMANDS for the team the command belongs to.

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionViewTeam) {
		// here we return Not_found instead of a permissions error so we don't leak the existence of
		// a command to someone without permissions for the team it belongs to.
		c.SetCommandNotFoundError()
		return
	}
	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageSlashCommands) {
		// again, return not_found to ensure id existence does not leak.
		c.SetCommandNotFoundError()
		return
	}
	if err := json.NewEncoder(w).Encode(cmd); err != nil {
		mlog.Warn("Error while writing response", mlog.Err(err))
	}
}

func executeCommand(c *Context, w http.ResponseWriter, r *http.Request) {
	commandArgs := model.CommandArgsFromJson(r.Body)
	if commandArgs == nil {
		c.SetInvalidParam("command_args")
		return
	}

	if len(commandArgs.Command) <= 1 || strings.Index(commandArgs.Command, "/") != 0 || !model.IsValidId(commandArgs.ChannelId) {
		c.Err = model.NewAppError("executeCommand", "api.command.execute_command.start.app_error", nil, "", http.StatusBadRequest)
		return
	}

	auditRec := c.MakeAuditRecord("executeCommand", audit.Fail)
	defer c.LogAuditRec(auditRec)
	auditRec.AddMeta("commandargs", commandArgs)

	// checks that user is a member of the specified channel, and that they have permission to use slash commands in it
	if !c.App.SessionHasPermissionToChannel(*c.AppContext.Session(), commandArgs.ChannelId, model.PermissionUseSlashCommands) {
		c.SetPermissionError(model.PermissionUseSlashCommands)
		return
	}

	channel, err := c.App.GetChannel(commandArgs.ChannelId)
	if err != nil {
		c.Err = err
		return
	}

	if channel.Type != model.ChannelTypeDirect && channel.Type != model.ChannelTypeGroup {
		// if this isn't a DM or GM, the team id is implicitly taken from the channel so that slash commands created on
		// some other team can't be run against this one
		commandArgs.TeamId = channel.TeamId
	} else {
		// if the slash command was used in a DM or GM, ensure that the user is a member of the specified team, so that
		// they can't just execute slash commands against arbitrary teams
		if c.AppContext.Session().GetTeamByTeamId(commandArgs.TeamId) == nil {
			if !c.App.SessionHasPermissionTo(*c.AppContext.Session(), model.PermissionUseSlashCommands) {
				c.SetPermissionError(model.PermissionUseSlashCommands)
				return
			}
		}
	}

	commandArgs.UserId = c.AppContext.Session().UserId
	commandArgs.T = c.AppContext.T
	commandArgs.SiteURL = c.GetSiteURLHeader()
	commandArgs.Session = *c.AppContext.Session()

	auditRec.AddMeta("commandargs", commandArgs) // overwrite in case teamid changed

	response, err := c.App.ExecuteCommand(c.AppContext, commandArgs)
	if err != nil {
		c.Err = err
		return
	}

	auditRec.Success()
	if err := json.NewEncoder(w).Encode(response); err != nil {
		mlog.Warn("Error while writing response", mlog.Err(err))
	}
}

func listAutocompleteCommands(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireTeamId()
	if c.Err != nil {
		return
	}

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), c.Params.TeamId, model.PermissionViewTeam) {
		c.SetPermissionError(model.PermissionViewTeam)
		return
	}

	commands, err := c.App.ListAutocompleteCommands(c.Params.TeamId, c.AppContext.T)
	if err != nil {
		c.Err = err
		return
	}

	if err := json.NewEncoder(w).Encode(commands); err != nil {
		mlog.Warn("Error while writing response", mlog.Err(err))
	}
}

func listCommandAutocompleteSuggestions(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireTeamId()
	if c.Err != nil {
		return
	}
	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), c.Params.TeamId, model.PermissionViewTeam) {
		c.SetPermissionError(model.PermissionViewTeam)
		return
	}

	roleId := model.SystemUserRoleId
	if c.IsSystemAdmin() {
		roleId = model.SystemAdminRoleId
	}

	query := r.URL.Query()
	userInput := query.Get("user_input")
	if userInput == "" {
		c.SetInvalidParam("userInput")
		return
	}
	userInput = strings.TrimPrefix(userInput, "/")

	commands, err := c.App.ListAutocompleteCommands(c.Params.TeamId, c.AppContext.T)
	if err != nil {
		c.Err = err
		return
	}

	commandArgs := &model.CommandArgs{
		ChannelId: query.Get("channel_id"),
		TeamId:    c.Params.TeamId,
		RootId:    query.Get("root_id"),
		ParentId:  query.Get("parent_id"),
		UserId:    c.AppContext.Session().UserId,
		T:         c.AppContext.T,
		Session:   *c.AppContext.Session(),
		SiteURL:   c.GetSiteURLHeader(),
		Command:   userInput,
	}

	suggestions := c.App.GetSuggestions(c.AppContext, commandArgs, commands, roleId)

	w.Write(model.AutocompleteSuggestionsToJSON(suggestions))
}

func regenCommandToken(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireCommandId()
	if c.Err != nil {
		return
	}

	auditRec := c.MakeAuditRecord("regenCommandToken", audit.Fail)
	defer c.LogAuditRec(auditRec)
	c.LogAudit("attempt")

	cmd, err := c.App.GetCommand(c.Params.CommandId)
	if err != nil {
		auditRec.AddMeta("command_id", c.Params.CommandId)
		c.SetCommandNotFoundError()
		return
	}
	auditRec.AddMeta("command", cmd)

	if !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		// here we return Not_found instead of a permissions error so we don't leak the existence of
		// a command to someone without permissions for the team it belongs to.
		c.SetCommandNotFoundError()
		return
	}

	if c.AppContext.Session().UserId != cmd.CreatorId && !c.App.SessionHasPermissionToTeam(*c.AppContext.Session(), cmd.TeamId, model.PermissionManageOthersSlashCommands) {
		c.LogAudit("fail - inappropriate permissions")
		c.SetPermissionError(model.PermissionManageOthersSlashCommands)
		return
	}

	rcmd, err := c.App.RegenCommandToken(cmd)
	if err != nil {
		c.Err = err
		return
	}

	auditRec.Success()
	c.LogAudit("success")

	resp := make(map[string]string)
	resp["token"] = rcmd.Token

	w.Write([]byte(model.MapToJson(resp)))
}
