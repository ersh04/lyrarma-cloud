package webui

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func (h *Handler) createFolder(c fiber.Ctx) error {
	token := h.token(c)
	currentFolderID := models.NormalizeFolderID(c.Query("folder_id"))
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(currentFolderID) + "&error=folder_name")
	}

	form := url.Values{}
	form.Set("name", name)
	form.Set("parent_id", currentFolderID)
	response, err := h.callAPI(c, fiber.MethodPost, "/api/folders/create",
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", token)
	if err != nil {
		h.logger.Error("folder creation failed", "err", err)
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(currentFolderID) + "&error=connect")
	}
	if response.status == fiber.StatusUnauthorized {
		return h.expireSession(c)
	}
	if response.status != fiber.StatusCreated {
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(currentFolderID) + "&error=folder_create")
	}

	return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(currentFolderID) + "&folder_created=" + url.QueryEscape(name))
}

func (h *Handler) changeFolderPermission(c fiber.Ctx) error {
	token := h.token(c)
	folderID := url.PathEscape(c.Params("folderID"))
	currentFolderID := models.NormalizeFolderID(c.Query("folder_id"))
	isPublic := c.Query("is_folder_public") == "true"

	target := "/api/folders/" + folderID + "/change_permission/"
	if isPublic {
		target += "true"
	} else {
		target += "false"
	}
	response, err := h.callAPI(c, fiber.MethodPost, target, nil, "", token)
	if err != nil || response.status >= fiber.StatusBadRequest {
		h.logger.Warn("failed to update folder access", "folder_id", folderID, "status", response.status, "err", err)
	}
	return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(currentFolderID))
}

func (h *Handler) deleteFolder(c fiber.Ctx) error {
	token := h.token(c)
	folderID := url.PathEscape(c.Params("folderID"))
	currentFolderID := models.NormalizeFolderID(c.Query("folder_id"))

	response, err := h.callAPI(c, fiber.MethodDelete, "/api/folders/"+folderID, nil, "", token)
	if err != nil || response.status >= fiber.StatusBadRequest {
		h.logger.Warn("failed to delete folder", "folder_id", folderID, "status", response.status, "err", err)
	}
	if currentFolderID == c.Params("folderID") {
		currentFolderID = "root"
	}
	return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(currentFolderID) + "&folder_deleted=1")
}
