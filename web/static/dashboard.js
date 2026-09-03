(function () {
  const dashboardRoot = document.querySelector(".dashboard");

  function getElement(id) {
    return document.getElementById(id);
  }

  function translate(name, values = {}) {
    let message = dashboardRoot?.dataset[name] || "";
    Object.entries(values).forEach(([key, value]) => {
      message = message.replaceAll(`{${key}}`, String(value));
    });
    return message;
  }

  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }

    const helper = document.createElement("textarea");
    helper.value = text;
    helper.setAttribute("readonly", "readonly");
    helper.style.position = "absolute";
    helper.style.left = "-9999px";
    document.body.appendChild(helper);
    helper.select();
    document.execCommand("copy");
    document.body.removeChild(helper);
    return true;
  }

  function formatBytes(bytes) {
    const units = ["B", "KB", "MB", "GB"];
    let index = 0;

    while (bytes >= 1024 && index < units.length - 1) {
      bytes /= 1024;
      index += 1;
    }

    return `${bytes.toFixed(1)} ${units[index]}`;
  }

  function describeSelectedFiles(files) {
    if (!files.length) {
      return "";
    }

    if (files.length === 1) {
      return `<strong>${files[0].name}</strong><br/><span class="dropzone__hint">${formatBytes(files[0].size)}</span>`;
    }

    const totalBytes = files.reduce((sum, file) => sum + file.size, 0);
    return `<strong>${translate("selectedFiles", { count: files.length })}</strong><br/><span class="dropzone__hint">${translate("totalSize", { size: formatBytes(totalBytes) })}</span>`;
  }

  function uniqueFiles(files) {
    const seen = new Set();
    return files.filter((file) => {
      const key = [file.name, file.size, file.lastModified, file.type, file.webkitRelativePath].join("\u0000");
      if (seen.has(key)) {
        return false;
      }
      seen.add(key);
      return true;
    });
  }

  function setSelectedFolder(folderId) {
    const uploadFolderId = getElement("uploadFolderId");
    const normalizedFolderId = folderId || "root";

    if (uploadFolderId) {
      uploadFolderId.value = normalizedFolderId;
    }

    document.querySelectorAll(".folder-item").forEach((item) => {
      item.classList.toggle("folder-item--selected", item.dataset.folderId === normalizedFolderId);
    });
  }

  function openFolder(folderId) {
    const normalizedFolderId = folderId || "root";
    const url = new URL(window.location.href);
    url.searchParams.set("folder_id", normalizedFolderId);
    window.location.href = url.toString();
  }

  function togglePermission(fileId, isPublic, folderId) {
    const url = new URL(`${window.location.origin}/change_permission/${encodeURIComponent(fileId)}`);
    url.searchParams.set("is_file_public", String(Boolean(isPublic)));
    url.searchParams.set("folder_id", folderId || "root");

    return fetch(url.toString(), {
      method: "GET",
      headers: {
        "X-Requested-With": "XMLHttpRequest",
      },
      redirect: "follow",
    });
  }

  function toggleFolderPermission(folderId, isPublic, currentFolderId) {
    const url = new URL(`${window.location.origin}/folders/change_permission/${encodeURIComponent(folderId)}`);
    url.searchParams.set("is_folder_public", String(Boolean(isPublic)));
    url.searchParams.set("folder_id", currentFolderId || "root");

    return fetch(url.toString(), {
      method: "GET",
      headers: {
        "X-Requested-With": "XMLHttpRequest",
      },
      redirect: "follow",
    });
  }

  function deleteFolder(folderId, currentFolderId) {
    const url = new URL(`${window.location.origin}/folders/delete/${encodeURIComponent(folderId)}`);
    url.searchParams.set("folder_id", currentFolderId || "root");

    return fetch(url.toString(), {
      method: "POST",
      headers: {
        "X-Requested-With": "XMLHttpRequest",
      },
      redirect: "follow",
    });
  }

  function bindFolderItems() {
    document.querySelectorAll(".folder-item").forEach((item) => {
      const deleteForm = item.querySelector("form");
      if (deleteForm) {
        deleteForm.addEventListener("click", (event) => {
          event.stopPropagation();
        });

        deleteForm.addEventListener("submit", async (event) => {
          event.preventDefault();
          event.stopPropagation();

          const folderId = item.dataset.folderId || "";
          const currentFolderId = dashboardRoot?.dataset.currentFolderId || "root";
          const folderName = item.dataset.folderName || translate("folderFallback");

          const confirmed = window.confirm(translate("confirmDeleteFolder", { name: folderName }));
          if (!confirmed) {
            return;
          }

          const submitButton = deleteForm.querySelector('button[type="submit"]');
          if (submitButton) {
            submitButton.disabled = true;
          }

          try {
            const response = await deleteFolder(folderId, currentFolderId);
            if (!response.ok) {
              throw new Error(`HTTP ${response.status}`);
            }

            item.remove();
          } catch (error) {
            if (submitButton) {
              submitButton.disabled = false;
            }
            alert(translate("errorDeleteFolderRetry"));
          }
        });
      }

      const selectFolder = () => {
        const folderId = item.dataset.folderId || "";
        setSelectedFolder(folderId);
        openFolder(folderId);
      };

      item.addEventListener("click", selectFolder);
      item.addEventListener("keydown", (event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          selectFolder();
        }
      });
    });
  }

  function bindPermissionSwitches() {
    document.querySelectorAll(".permission-switch__input").forEach((input) => {
      input.addEventListener("change", () => {
        const fileId = input.dataset.fileId;
        const folderId = input.dataset.folderId || "root";
        const folderPermissionId = input.dataset.folderId;
        const currentFolderId = input.dataset.currentFolderId || "root";

        if (fileId) {
          input.disabled = true;
          const nextState = input.checked;
          togglePermission(fileId, nextState, folderId)
            .then((response) => {
              if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
              }

              const shareButton = document.querySelector(`.file-table__share-btn[data-file-id="${CSS.escape(fileId)}"]`);
              if (shareButton) {
                shareButton.dataset.isPublic = String(nextState);
              }
            })
            .catch(() => {
              input.checked = !nextState;
              alert(translate("errorFilePermissionRetry"));
            })
            .finally(() => {
              input.disabled = false;
            });
          return;
        }

        if (folderPermissionId) {
          input.disabled = true;
          const nextState = input.checked;
          toggleFolderPermission(folderPermissionId, nextState, currentFolderId)
            .then((response) => {
              if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
              }
            })
            .catch(() => {
              input.checked = !nextState;
              alert(translate("errorFolderPermissionRetry"));
            })
            .finally(() => {
              input.disabled = false;
            });
          return;
        }
      });

      const switchRoot = input.closest(".permission-switch");
      if (switchRoot) {
        switchRoot.addEventListener("click", (event) => {
          event.stopPropagation();
        });
      }
    });
  }

  function bindShareButtons() {
    document.querySelectorAll(".file-table__share-btn").forEach((button) => {
      button.addEventListener("click", async () => {
        const isPublic = button.dataset.isPublic === "true";
        if (!isPublic) {
          alert(translate("shareMakePublicFirst"));
          return;
        }

        const publicUrl = new URL(button.dataset.publicUrl || "/", window.location.origin).toString();

        try {
          await copyText(publicUrl);
          alert(translate("shareLinkCopied"));
        } catch (error) {
          window.prompt(translate("copyLinkPrompt"), publicUrl);
        }
      });
    });
  }

  function bindFileDeleteForms() {
    document.querySelectorAll('form.inline-form[action^="/delete/"]').forEach((deleteForm) => {
      deleteForm.addEventListener("submit", async (event) => {
        event.preventDefault();

        const action = deleteForm.getAttribute("action") || "";
        const row = deleteForm.closest("tr");
        const fileNameEl = row?.querySelector(".file-table__name-text");
        const fileName = fileNameEl?.textContent?.trim() || translate("fileFallback");

        const confirmed = window.confirm(translate("confirmDeleteFile", { name: fileName }));
        if (!confirmed) {
          return;
        }

        const submitButton = deleteForm.querySelector('button[type="submit"]');
        if (submitButton) {
          submitButton.disabled = true;
        }

        try {
          const response = await fetch(action, {
            method: "POST",
            headers: {
              "X-Requested-With": "XMLHttpRequest",
            },
            redirect: "follow",
          });

          if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
          }

          if (row) {
            row.remove();
          }

          const tableBody = document.querySelector(".file-table tbody");
          const hasRows = Boolean(tableBody && tableBody.querySelector("tr"));
          if (!hasRows) {
            const tableCard = document.querySelector(".card--table");
            if (tableCard) {
              tableCard.remove();
            }

            const workspaceContent = document.querySelector(".file-manager__content");
            const emptyStateExists = Boolean(document.querySelector(".empty-state.card"));
            if (workspaceContent && !emptyStateExists) {
              const emptyState = document.createElement("div");
              emptyState.className = "empty-state card";
              const emptyText = translate("emptyFilesText");
              const emptyHint = translate("emptyFilesHint");

              emptyState.innerHTML = `
                <div class="empty-state__icon">📭</div>
                <p class="empty-state__text">${emptyText}</p>
                <p class="empty-state__hint">${emptyHint}</p>
              `;
              workspaceContent.appendChild(emptyState);
            }
          }
        } catch (error) {
          if (submitButton) {
            submitButton.disabled = false;
          }
          alert(translate("errorDeleteFileRetry"));
        }
      });
    });
  }

  function bindUploadForm() {
    const dropzone = getElement("dropzone");
    const fileInput = getElement("fileInput");
    const dropText = getElement("dropzoneText");
    const form = getElement("uploadForm");
    const uploadBtn = getElement("uploadBtn");
    const progressWrap = getElement("progressWrap");
    const progressBar = getElement("progressBar");
    const progressLbl = getElement("progressLabel");
    const uploadFolderId = getElement("uploadFolderId");

    if (!dashboardRoot || !dropzone || !fileInput || !dropText || !form || !uploadBtn || !progressWrap || !progressBar || !progressLbl || !uploadFolderId || form.dataset.uploadBound === "true") {
      return;
    }
    form.dataset.uploadBound = "true";

    const maxUploadMb = Number(dashboardRoot.dataset.maxUploadMb || "1024");
    const maxUploadBytes = Math.max(1, maxUploadMb) * 1024 * 1024;

    fileInput.addEventListener("change", () => {
      const selectedFiles = uniqueFiles(Array.from(fileInput.files || []));
      if (selectedFiles.length > 0) {
        dropText.innerHTML = describeSelectedFiles(selectedFiles);
      }
    });

    dropzone.addEventListener("dragover", (event) => {
      event.preventDefault();
      dropzone.classList.add("dropzone--active");
    });

    ["dragleave", "dragend"].forEach((evt) => {
      dropzone.addEventListener(evt, () => dropzone.classList.remove("dropzone--active"));
    });

    dropzone.addEventListener("drop", (event) => {
      event.preventDefault();
      dropzone.classList.remove("dropzone--active");

      if (event.dataTransfer.files.length > 0) {
        const dt = new DataTransfer();
        Array.from(event.dataTransfer.files).forEach((file) => {
          dt.items.add(file);
        });
        fileInput.files = dt.files;
        fileInput.dispatchEvent(new Event("change"));
      }
    });

    form.addEventListener("submit", async (event) => {
      event.preventDefault();

      if (form.dataset.uploading === "true") {
        return;
      }

      const selectedFiles = uniqueFiles(Array.from(fileInput.files || []));
      if (!selectedFiles.length) {
        fileInput.click();
        return;
      }

      for (const selectedFile of selectedFiles) {
        if (selectedFile.size > maxUploadBytes) {
          progressWrap.hidden = false;
          progressBar.style.width = "0%";
          progressLbl.textContent = translate("fileTooLargeDetail", { size: formatBytes(selectedFile.size), limit: maxUploadMb });
          return;
        }
      }

      form.dataset.uploading = "true";
      uploadBtn.disabled = true;
      progressWrap.hidden = false;

      const totalBytes = selectedFiles.reduce((sum, file) => sum + file.size, 0);
      const uploadedRatios = new Array(selectedFiles.length).fill(0);
      const completedFiles = new Array(selectedFiles.length).fill(false);

      const updateProgress = (fileName) => {
        const uploadedBytes = selectedFiles.reduce(
          (sum, file, index) => sum + file.size * uploadedRatios[index],
          0,
        );
        const pct = totalBytes > 0 ? Math.min(99, Math.round((uploadedBytes / totalBytes) * 100)) : 0;
        progressBar.style.width = `${pct}%`;
        progressLbl.textContent = translate("uploadProgress", { progress: pct, name: fileName });
      };

      const uploadFile = (selectedFile, fileIndex) => new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();

        xhr.upload.addEventListener("progress", (ev) => {
          if (ev.lengthComputable) {
            uploadedRatios[fileIndex] = ev.total > 0 ? Math.min(1, ev.loaded / ev.total) : 0;
            updateProgress(selectedFile.name);
          }
        });

        xhr.addEventListener("load", () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            uploadedRatios[fileIndex] = 1;
            completedFiles[fileIndex] = true;
            updateProgress(selectedFile.name);
            resolve();
            return;
          }

          if (xhr.status === 401) {
            reject(new Error("unauthorized"));
            return;
          }

          if (xhr.status === 413) {
            reject(new Error(`limit:${selectedFile.name}`));
            return;
          }

          reject(new Error(`http:${xhr.status}`));
        });

        xhr.addEventListener("error", () => {
          reject(new Error("network"));
        });

        const uploadUrl = `/upload?folder_id=${encodeURIComponent(uploadFolderId.value || "root")}`;
        const formData = new FormData();
        formData.set("file", selectedFile, selectedFile.name);

        xhr.open("POST", uploadUrl);
        xhr.setRequestHeader("X-Requested-With", "XMLHttpRequest");
        xhr.setRequestHeader("Accept", "application/json");
        xhr.send(formData);
      });

      const uploadBatch = async () => {
        let nextIndex = 0;
        let firstError = null;

        const worker = async () => {
          while (true) {
            if (firstError) {
              return;
            }

            const fileIndex = nextIndex;
            nextIndex += 1;
            if (fileIndex >= selectedFiles.length) {
              return;
            }

            try {
              await uploadFile(selectedFiles[fileIndex], fileIndex);
            } catch (error) {
              if (!firstError) {
                firstError = error;
              }
              return;
            }
          }
        };

        const workerCount = Math.min(3, selectedFiles.length);
        await Promise.all(Array.from({ length: workerCount }, () => worker()));

        if (firstError) {
          throw firstError;
        }
      };

      try {
        await uploadBatch();

        progressBar.style.width = "100%";
        progressLbl.textContent = translate("uploadComplete", { count: selectedFiles.length });

        const redirectUrl = new URL("/dashboard", window.location.origin);
        redirectUrl.searchParams.set("folder_id", uploadFolderId.value || "root");
        if (selectedFiles.length === 1) {
          redirectUrl.searchParams.set("uploaded", selectedFiles[0].name);
        }
        window.location.href = redirectUrl.toString();
      } catch (error) {
        const errorMessage = String(error && error.message || "");
        if (errorMessage === "unauthorized") {
          window.location.href = "/login?expired=1";
          return;
        }

        const remainingFiles = selectedFiles.filter((_, index) => !completedFiles[index]);
        if (remainingFiles.length > 0) {
          const remainingSelection = new DataTransfer();
          remainingFiles.forEach((file) => remainingSelection.items.add(file));
          fileInput.files = remainingSelection.files;
          fileInput.dispatchEvent(new Event("change"));
        }

        if (errorMessage.startsWith("limit:")) {
          const fileName = errorMessage.slice("limit:".length);
          progressLbl.textContent = translate("uploadRejected", { name: fileName, limit: maxUploadMb });
        } else if (errorMessage === "network") {
          progressLbl.textContent = translate("networkError");
        } else {
          progressLbl.textContent = translate("uploadError");
        }
        delete form.dataset.uploading;
        uploadBtn.disabled = false;
      }
    });
  }

  function initialize() {
    if (!dashboardRoot) {
      return;
    }

    bindFolderItems();
    bindPermissionSwitches();
    bindShareButtons();
    bindFileDeleteForms();
    bindUploadForm();
    setSelectedFolder(dashboardRoot.dataset.currentFolderId || "root");
  }

  initialize();
})();