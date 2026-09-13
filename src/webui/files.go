package webui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func (h *Handler) dashboard(c fiber.Ctx) error {
	token := h.token(c)
	maxUploadSize := h.config.MaxUploadSize

	currentFolderID := strings.TrimSpace(c.Query("folder_id"))
	if currentFolderID == "" {
		currentFolderID = "root"
	}

	filesResponse, err := h.callAPI(c, fiber.MethodGet,
		"/api/files?folder_id="+url.QueryEscape(currentFolderID), nil, "", token)
	if err != nil {
		h.logger.Error("failed to retrieve files", "err", err)
		return h.renderDashboard(c, nil, nil, currentFolderID, maxUploadSize, h.message(c, "error_files_list"))
	}
	if filesResponse.status == fiber.StatusUnauthorized {
		return h.expireSession(c)
	}

	foldersResponse, err := h.callAPI(c, fiber.MethodGet, "/api/folders", nil, "", token)
	if err != nil {
		h.logger.Error("failed to retrieve folders", "err", err)
		return h.renderDashboard(c, nil, nil, currentFolderID, maxUploadSize, h.message(c, "error_folders_list"))
	}
	if foldersResponse.status == fiber.StatusUnauthorized {
		return h.expireSession(c)
	}

	limitsResponse, limitsErr := h.callAPI(c, fiber.MethodGet, "/api/info/limits", nil, "", token)
	if limitsErr != nil {
		h.logger.Error("failed to retrieve user limits", "err", limitsErr)
	} else if limitsResponse.status == fiber.StatusUnauthorized {
		return h.expireSession(c)
	} else if limitsResponse.status == fiber.StatusOK {
		var limits struct {
			MaxUploadSize int64 `json:"max_upload_size"`
		}
		if json.Unmarshal(limitsResponse.body, &limits) == nil && limits.MaxUploadSize > 0 {
			maxUploadSize = limits.MaxUploadSize
		} else {
			h.logger.Error("failed to decode user limits")
		}
	} else {
		h.logger.Error("failed to retrieve user limits", "status", limitsResponse.status)
	}

	var fileList models.FileListResponse
	var folderList models.FolderListResponse
	if filesResponse.status != fiber.StatusOK || json.Unmarshal(filesResponse.body, &fileList) != nil {
		return h.renderDashboard(c, nil, nil, currentFolderID, maxUploadSize, h.apiMessage(c, filesResponse, "error_files_list"))
	}
	if foldersResponse.status != fiber.StatusOK || json.Unmarshal(foldersResponse.body, &folderList) != nil {
		return h.renderDashboard(c, fileList.Files, nil, currentFolderID, maxUploadSize, h.apiMessage(c, foldersResponse, "error_folders_list"))
	}

	return h.renderDashboard(c, fileList.Files, folderList.Folders, currentFolderID, maxUploadSize, h.dashboardError(c))
}

func (h *Handler) renderDashboard(
	c fiber.Ctx,
	files []models.FileEntry,
	folders []models.FolderEntry,
	currentFolderID string,
	maxUploadSize int64,
	errorMessage string,
) error {
	folderByID := make(map[string]models.FolderEntry, len(folders))
	for _, folder := range folders {
		folderByID[folder.ID] = folder
	}

	currentFolderName := h.translate(h.requestLanguage(c), "root_folder")
	parentFolderID := ""
	if currentFolderID != "root" {
		current, found := folderByID[currentFolderID]
		if !found {
			currentFolderID = "root"
			errorMessage = h.message(c, "error_folder_not_found")
		} else {
			currentFolderName = current.Name
			parentFolderID = current.ParentID
		}
	}

	visibleFolders := make([]map[string]any, 0)
	allFolders := make([]map[string]any, 0, len(folders))
	for _, folder := range folders {
		item := folderContext(folder)
		allFolders = append(allFolders, item)
		if models.NormalizeFolderID(folder.ParentID) == currentFolderID {
			visibleFolders = append(visibleFolders, item)
		}
	}

	fileItems := make([]map[string]any, 0, len(files))
	for _, file := range files {
		fileItems = append(fileItems, fileContext(file))
	}

	return h.render(c, "dashboard.html", pongo2.Context{
		"files":               fileItems,
		"folders":             allFolders,
		"visible_folders":     visibleFolders,
		"breadcrumbs":         buildBreadcrumbs(folderByID, currentFolderID, h.translate(h.requestLanguage(c), "root_folder")),
		"parent_folder_id":    parentFolderID,
		"current_folder_id":   currentFolderID,
		"current_folder_name": currentFolderName,
		"error":               errorMessage,
		"notice":              h.dashboardNotice(c),
		"files_word":          h.countWord(h.requestLanguage(c), len(files), "file"),
		"folders_word":        h.countWord(h.requestLanguage(c), len(folders), "folder"),
		"max_upload_mb":       maxUploadSize / (1024 * 1024),
	}, fiber.StatusOK)
}

func (h *Handler) upload(c fiber.Ctx) error {
	token := h.token(c)
	isXHR := c.XHR()

	folderID := models.NormalizeFolderID(c.Query("folder_id"))
	target := "/api/files/upload?folder_id=" + url.QueryEscape(folderID)
	response, err := h.callAPI(c, fiber.MethodPost, target, c.Request().BodyStream(), c.Get(fiber.HeaderContentType), token)
	if err != nil {
		h.logger.Error("file upload failed", "err", err)
		if isXHR {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "connect"})
		}
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(folderID) + "&error=connect")
	}

	sendAPIResponse := func() error {
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
		return c.Status(response.status).Send(response.body)
	}

	if response.status == fiber.StatusUnauthorized {
		if isXHR {
			h.clearAuthCookies(c)
			return sendAPIResponse()
		}
		return h.expireSession(c)
	}
	if response.status == fiber.StatusRequestEntityTooLarge {
		if isXHR {
			return sendAPIResponse()
		}
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(folderID) + "&error=too_large")
	}
	if response.status != fiber.StatusCreated {
		if isXHR {
			return sendAPIResponse()
		}
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(folderID) + "&error=upload")
	}

	var entry models.FileEntry
	_ = json.Unmarshal(response.body, &entry)
	location := "/dashboard?folder_id=" + url.QueryEscape(folderID)
	if entry.OriginalName != "" {
		location += "&uploaded=" + url.QueryEscape(entry.OriginalName)
	}
	if isXHR {
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"file":     entry,
			"location": location,
		})
	}
	return c.Redirect().Status(fiber.StatusFound).To(location)
}

func (h *Handler) download(c fiber.Ctx) error {
	token := h.token(c)
	fileID := url.PathEscape(c.Params("fileID"))
	return h.proxyAPI(c, fiber.MethodGet, "/api/files/"+fileID+"/download", token)
}

func (h *Handler) publicDownload(c fiber.Ctx) error {
	fileID := url.PathEscape(c.Params("fileID"))
	return h.proxyAPI(c, fiber.MethodGet, "/api/public/files/"+fileID+"/download", "")
}

func (h *Handler) publicFolderDownload(c fiber.Ctx) error {
	folderID := url.PathEscape(c.Params("folderID"))
	return h.proxyAPI(c, fiber.MethodGet, "/api/public/folders/"+folderID+"/download", "")
}

func (h *Handler) changeFilePermission(c fiber.Ctx) error {
	token := h.token(c)
	fileID := url.PathEscape(c.Params("fileID"))
	isPublic := c.Query("is_file_public") == "true"
	folderID := models.NormalizeFolderID(c.Query("folder_id"))

	response, err := h.callAPI(c, fiber.MethodGet,
		fmt.Sprintf("/api/files/%s/change_permission/%t", fileID, isPublic), nil, "", token)
	if err != nil || response.status >= fiber.StatusBadRequest {
		h.logger.Warn("failed to update file access", "file_id", fileID, "status", response.status, "err", err)
	}
	return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(folderID))
}

func (h *Handler) deleteFile(c fiber.Ctx) error {
	token := h.token(c)
	fileID := url.PathEscape(c.Params("fileID"))
	folderID := models.NormalizeFolderID(c.Query("folder_id"))
	response, err := h.callAPI(c, fiber.MethodDelete, "/api/files/"+fileID, nil, "", token)
	if err != nil || response.status >= fiber.StatusBadRequest {
		h.logger.Warn("failed to delete file", "file_id", fileID, "status", response.status, "err", err)
	}
	return c.Redirect().Status(fiber.StatusFound).To("/dashboard?folder_id=" + url.QueryEscape(folderID))
}

func (h *Handler) sharedFile(c fiber.Ctx) error {
	fileID := url.PathEscape(c.Params("fileID"))
	response, err := h.callAPI(c, fiber.MethodGet, "/api/public/files/"+fileID+"/info", nil, "", "")
	file := map[string]any{
		"id":           fileID,
		"name":         "download",
		"size":         nil,
		"content_type": "application/octet-stream",
		"available":    false,
	}
	errorMessage := ""
	if err != nil {
		errorMessage = h.message(c, "error_public_file_open")
	} else if response.status == fiber.StatusOK {
		var entry models.FileEntry
		if json.Unmarshal(response.body, &entry) == nil {
			file = fileContext(entry)
			file["available"] = true
		}
	} else if response.status == fiber.StatusNotFound {
		errorMessage = h.message(c, "error_public_file_not_found")
	} else {
		errorMessage = h.apiMessage(c, response, "error_public_file_open")
	}

	return h.render(c, "shared_file.html", pongo2.Context{
		"file":         file,
		"error":        errorMessage,
		"download_url": "/public/download/" + fileID,
	}, fiber.StatusOK)
}

func (h *Handler) sharedFolder(c fiber.Ctx) error {
	folderID := url.PathEscape(c.Params("folderID"))
	folder := map[string]any{"id": folderID, "name": "Shared Folder", "available": false}
	errorMessage := h.sharedFolderError(c, c.Query("error"))

	response, err := h.callAPI(c, fiber.MethodGet, "/api/public/folders/"+folderID+"/info", nil, "", "")
	if err != nil {
		errorMessage = h.message(c, "error_public_folder_open")
	} else if response.status == fiber.StatusOK {
		var entry models.FolderEntry
		if json.Unmarshal(response.body, &entry) == nil {
			folder = folderContext(entry)
			folder["available"] = true
		}
	} else if response.status == fiber.StatusNotFound {
		errorMessage = h.message(c, "error_public_folder_not_found")
	} else {
		errorMessage = h.apiMessage(c, response, "error_public_folder_open")
	}

	return h.render(c, "shared_folder.html", pongo2.Context{
		"folder":              folder,
		"error":               errorMessage,
		"download_folder_url": "/public/folders/download/" + folderID,
		"open_dashboard_url":  "/dashboard?folder_id=" + folderID,
		"share_url":           requestURL(c),
	}, fiber.StatusOK)
}

func (h *Handler) expireSession(c fiber.Ctx) error {
	h.clearAuthCookies(c)
	return c.Redirect().Status(fiber.StatusFound).To("/login?expired=1")
}

func fileContext(file models.FileEntry) map[string]any {
	return map[string]any{
		"id":            file.ID,
		"owner_id":      file.OwnerID,
		"is_public":     file.IsPublic,
		"folder_id":     models.NormalizeFolderID(file.FolderID),
		"original_name": file.OriginalName,
		"name":          file.OriginalName,
		"size":          file.Size,
		"content_type":  file.ContentType,
		"uploaded_at":   file.UploadedAt.Format("2006-01-02"),
	}
}

func folderContext(folder models.FolderEntry) map[string]any {
	return map[string]any{
		"id":         folder.ID,
		"owner_id":   folder.OwnerID,
		"parent_id":  models.NormalizeFolderID(folder.ParentID),
		"name":       folder.Name,
		"is_public":  folder.IsPublic,
		"created_at": folder.CreatedAt.Format("2006-01-02"),
	}
}

func buildBreadcrumbs(folders map[string]models.FolderEntry, currentID, rootName string) []map[string]any {
	result := []map[string]any{{"id": "root", "name": rootName}}
	if currentID == "root" {
		return result
	}

	chain := make([]models.FolderEntry, 0)
	visited := make(map[string]bool)
	for id := currentID; id != "" && id != "root" && !visited[id]; {
		visited[id] = true
		folder, found := folders[id]
		if !found {
			break
		}
		chain = append(chain, folder)
		id = models.NormalizeFolderID(folder.ParentID)
	}
	for index := len(chain) - 1; index >= 0; index-- {
		result = append(result, map[string]any{"id": chain[index].ID, "name": chain[index].Name})
	}
	return result
}

func (h *Handler) dashboardError(c fiber.Ctx) string {
	key := map[string]string{
		"too_large":     "error_file_too_large",
		"upload":        "error_upload_failed",
		"connect":       "error_upload_server",
		"folder_name":   "error_folder_name",
		"folder_create": "error_folder_create",
	}[c.Query("error")]
	if key == "" {
		return ""
	}
	return h.message(c, key)
}

func (h *Handler) dashboardNotice(c fiber.Ctx) string {
	if name := c.Query("folder_created"); name != "" {
		return fmt.Sprintf(h.message(c, "notice_folder_created"), name)
	}
	if c.Query("folder_deleted") != "" {
		return h.message(c, "notice_folder_deleted")
	}
	if name := c.Query("uploaded"); name != "" {
		return fmt.Sprintf(h.message(c, "notice_file_uploaded"), name)
	}
	return ""
}

func (h *Handler) sharedFolderError(c fiber.Ctx, code string) string {
	key := map[string]string{
		"connect":              "error_public_folder_download",
		"not_found":            "error_public_folder_not_found",
		"download_unavailable": "error_folder_download_unavailable",
	}[code]
	if key == "" {
		return ""
	}
	return h.message(c, key)
}

func (h *Handler) countWord(lang string, count int, kind string) string {
	form := "many"
	if lang == "en" {
		if count == 1 {
			form = "one"
		} else {
			form = "other"
		}
	} else if count%10 == 1 && count%100 != 11 {
		form = "one"
	} else if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		form = "few"
	}
	return h.translate(lang, "count_"+kind+"_"+form)
}

func requestURL(c fiber.Ctx) string {
	return c.BaseURL() + path.Clean(c.Path())
}
