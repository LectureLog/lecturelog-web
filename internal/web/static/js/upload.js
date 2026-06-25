(function () {
  'use strict';

  document.addEventListener('DOMContentLoaded', function () {
    var root = document.getElementById('ll-upload');
    if (!root) {
      return;
    }

    var csrf = root.getAttribute('data-csrf') || '';
    var filePanel = root.querySelector('[data-panel="file"]');
    var urlPanel = root.querySelector('[data-panel="url"]');
    var modeButtons = Array.prototype.slice.call(root.querySelectorAll('[data-mode]'));
    var drop = root.querySelector('[data-drop]');
    var dropEmpty = root.querySelector('[data-drop-empty]');
    var fileCard = root.querySelector('[data-file-card]');
    var fileInput = root.querySelector('[data-file-input]');
    var fileName = root.querySelector('[data-file-name]');
    var fileSub = root.querySelector('[data-file-sub]');
    var pickFile = root.querySelector('[data-pick-file]');
    var clearFile = root.querySelector('[data-clear-file]');
    var fileSubmit = root.querySelector('[data-file-submit]');
    var submitLabel = root.querySelector('[data-submit-label]');
    var status = root.querySelector('[data-status]');
    var error = root.querySelector('[data-error]');
    var hasPDFInputs = Array.prototype.slice.call(root.querySelectorAll('[data-has-pdf]'));
    var extractInputs = Array.prototype.slice.call(root.querySelectorAll('[data-extract-slides]'));
    var selectedFile = null;
    var isSubmitting = false;
    var dragDepth = 0;
    var defaultSubmitText = submitLabel ? submitLabel.textContent : '';
    var defaultStatusText = status ? status.textContent : '';

    function setMode(mode) {
      modeButtons.forEach(function (button) {
        var active = button.getAttribute('data-mode') === mode;
        button.classList.toggle('ll-upload-seg-btn--active', active);
        button.setAttribute('aria-selected', active ? 'true' : 'false');
      });
      if (filePanel) {
        filePanel.classList.toggle('ll-upload-hidden', mode !== 'file');
      }
      if (urlPanel) {
        urlPanel.classList.toggle('ll-upload-hidden', mode !== 'url');
      }
      clearError();
    }

    function formatSize(bytes) {
      if (bytes >= 1073741824) {
        return (bytes / 1073741824).toFixed(1) + ' ГБ';
      }
      return Math.max(1, Math.round(bytes / 1048576)) + ' МБ';
    }

    function isMedia(file) {
      var name = file.name || '';
      var type = file.type || '';
      return /^(audio|video)\//.test(type) || /\.(mp4|mov|m4v|webm|mkv|mp3|wav|m4a|aac|ogg|flac)$/i.test(name);
    }

    function mediaKind(file) {
      var name = file.name || '';
      var type = file.type || '';
      if (/^audio\//.test(type) || /\.(mp3|wav|m4a|aac|ogg|flac)$/i.test(name)) {
        return 'аудио';
      }
      return 'видео';
    }

    function clearError() {
      if (error) {
        error.textContent = '';
        error.classList.remove('ll-upload-errnote--show');
      }
      if (drop) {
        drop.classList.remove('ll-upload-drop--error');
      }
    }

    function showError(message) {
      if (error) {
        error.textContent = message;
        error.classList.add('ll-upload-errnote--show');
      }
      if (drop) {
        drop.classList.add('ll-upload-drop--error');
      }
    }

    function resetSubmit() {
      if (fileSubmit) {
        fileSubmit.disabled = isSubmitting || !selectedFile;
      }
      if (submitLabel) {
        submitLabel.textContent = defaultSubmitText;
      }
      if (status) {
        status.textContent = defaultStatusText;
      }
    }

    function setBusy(text, note) {
      if (fileSubmit) {
        fileSubmit.disabled = true;
      }
      if (submitLabel) {
        submitLabel.textContent = text;
      }
      if (status) {
        status.textContent = note;
      }
    }

    function showFile(file) {
      if (isSubmitting) {
        return;
      }
      selectedFile = file;
      clearError();
      if (fileName) {
        fileName.textContent = file.name;
      }
      if (fileSub) {
        fileSub.textContent = mediaKind(file) + ' · ' + formatSize(file.size) + ' · готов к обработке';
      }
      if (dropEmpty) {
        dropEmpty.classList.add('ll-upload-hidden');
      }
      if (fileCard) {
        fileCard.classList.remove('ll-upload-hidden');
      }
      if (fileSubmit) {
        fileSubmit.disabled = false;
      }
    }

    function resetFile() {
      if (isSubmitting) {
        return;
      }
      selectedFile = null;
      clearError();
      if (dropEmpty) {
        dropEmpty.classList.remove('ll-upload-hidden');
      }
      if (fileCard) {
        fileCard.classList.add('ll-upload-hidden');
      }
      if (fileInput) {
        fileInput.value = '';
      }
      resetSubmit();
    }

    function takeFile(file) {
      if (isSubmitting) {
        return;
      }
      if (!file) {
        return;
      }
      if (!isMedia(file)) {
        resetFile();
        showError('Формат не поддерживается. Нужен аудио- или видеофайл: MP4, MOV, MP3, WAV или M4A.');
        return;
      }
      showFile(file);
    }

    function errorMessage(res, text, fallback) {
      if (res.status === 422) {
        return text || 'Формат или размер файла не поддерживается.';
      }
      if (res.status === 403) {
        return text || 'Доступ запрещён. Обновите страницу и попробуйте ещё раз.';
      }
      return text || fallback;
    }

    function postJSON(url, body) {
      return fetch(url, {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrf
        },
        body: JSON.stringify(body)
      });
    }

    function postForm(url, fields) {
      return fetch(url, {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'X-CSRF-Token': csrf
        },
        body: new URLSearchParams(fields)
      });
    }

    function currentFileOptions() {
      var hasPDF = root.querySelector('[data-panel="file"] [data-has-pdf]');
      var extract = root.querySelector('[data-panel="file"] [data-extract-slides]');
      return {
        hasPDF: hasPDF && hasPDF.checked,
        extractSlides: extract && extract.checked && !(hasPDF && hasPDF.checked)
      };
    }

    function submitFile() {
      if (isSubmitting) {
        return;
      }

      var file = selectedFile;
      if (!file) {
        showError('Выберите аудио- или видеофайл.');
        return;
      }

      isSubmitting = true;
      clearError();
      setBusy('Загрузка…', 'Получаем ссылку для загрузки.');

      var mime = file.type || 'application/octet-stream';
      var title = file.name;
      postJSON('/upload/presign', {
        filename: file.name,
        size: file.size,
        mime: mime
      }).then(function (res) {
        if (!res.ok) {
          return res.text().then(function (text) {
            throw new Error(errorMessage(res, text, 'Не удалось подготовить загрузку.'));
          });
        }
        return res.json();
      }).then(function (presign) {
        title = presign.title || file.name;
        setBusy('Загрузка…', 'Передаём файл в хранилище.');
        return fetch(presign.put_url, {
          method: 'PUT',
          credentials: 'omit',
          headers: {
            'Content-Type': mime
          },
          body: file
        }).then(function (res) {
          if (!res.ok) {
            throw new Error('Не удалось загрузить файл. Попробуйте ещё раз.');
          }
          return presign;
        });
      }).then(function (presign) {
        var opts = currentFileOptions();
        setBusy('Создаём конспект…', 'Запускаем обработку лекции.');
        return postForm('/upload/confirm', {
          token: presign.token || '',
          s3_key: presign.s3_key || '',
          title: title,
          has_pdf: opts.hasPDF ? 'true' : '',
          extract_slides: opts.extractSlides ? 'true' : ''
        });
      }).then(function (res) {
        if (!res.ok) {
          return res.text().then(function (text) {
            throw new Error(errorMessage(res, text, 'Не удалось создать задачу обработки.'));
          });
        }
        window.location.assign(res.headers.get('HX-Redirect') || '/lectures');
      }).catch(function (err) {
        showError(err.message || 'Не удалось загрузить файл.');
      }).finally(function () {
        isSubmitting = false;
        resetSubmit();
      });
    }

    function syncSlidesOptions(source) {
      var checked = source.checked;
      hasPDFInputs.forEach(function (input) {
        if (input !== source) {
          input.checked = checked;
        }
      });
      extractInputs.forEach(function (input) {
        input.disabled = checked;
        if (checked) {
          input.checked = false;
        }
      });
    }

    modeButtons.forEach(function (button) {
      button.addEventListener('click', function () {
        setMode(button.getAttribute('data-mode'));
      });
    });

    hasPDFInputs.forEach(function (input) {
      input.addEventListener('change', function () {
        syncSlidesOptions(input);
      });
    });

    if (pickFile && fileInput) {
      pickFile.addEventListener('click', function () {
        if (isSubmitting) {
          return;
        }
        fileInput.click();
      });
      fileInput.addEventListener('change', function () {
        takeFile(fileInput.files && fileInput.files[0]);
      });
    }

    if (clearFile) {
      clearFile.addEventListener('click', resetFile);
    }

    if (drop) {
      ['dragenter', 'dragover'].forEach(function (eventName) {
        drop.addEventListener(eventName, function (event) {
          event.preventDefault();
          if (eventName === 'dragenter') {
            dragDepth += 1;
          }
          if (isSubmitting) {
            return;
          }
          drop.classList.add('ll-upload-drop--over');
        });
      });
      drop.addEventListener('dragleave', function (event) {
        event.preventDefault();
        dragDepth -= 1;
        if (dragDepth <= 0) {
          dragDepth = 0;
          drop.classList.remove('ll-upload-drop--over');
        }
      });
      drop.addEventListener('drop', function (event) {
        event.preventDefault();
        if (isSubmitting) {
          dragDepth = 0;
          drop.classList.remove('ll-upload-drop--over');
          return;
        }
        dragDepth = 0;
        drop.classList.remove('ll-upload-drop--over');
        takeFile(event.dataTransfer.files && event.dataTransfer.files[0]);
      });
    }

    if (fileSubmit) {
      fileSubmit.addEventListener('click', submitFile);
    }

    syncSlidesOptions(hasPDFInputs[0] || { checked: false });
    setMode('file');
    resetSubmit();
  });
}());
