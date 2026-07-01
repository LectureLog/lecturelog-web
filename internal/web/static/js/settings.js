(function () {
  'use strict';

  document.addEventListener('DOMContentLoaded', function () {
    var root = document.getElementById('ll-settings');
    if (!root) {
      return;
    }

    // Показываем имя выбранного файла рядом с кастомной кнопкой «Выбрать файл».
    var fileInput = root.querySelector('#cookie-file');
    var fileName = root.querySelector('[data-settings-file-name]');
    if (fileInput && fileName) {
      fileInput.addEventListener('change', function () {
        var file = fileInput.files && fileInput.files[0];
        fileName.textContent = file ? file.name : 'Файл не выбран';
      });
    }

    // Приглушаем кнопку «Загрузить» на время htmx-запроса (индикатор уже показывает спиннер).
    var form = root.querySelector('.ll-settings-form');
    if (form) {
      var submitBtn = form.querySelector('.ll-settings-cta');
      form.addEventListener('htmx:beforeRequest', function () {
        if (submitBtn) {
          submitBtn.disabled = true;
        }
      });
      form.addEventListener('htmx:afterRequest', function () {
        if (submitBtn) {
          submitBtn.disabled = false;
        }
      });
    }
  });
})();
