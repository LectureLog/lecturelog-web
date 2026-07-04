# Задача 1 — Бэкенд: защита в глубину + тесты

## Статус: готово

## Коммит
`b5c0586` — `feat(upload): форсировать отключение извлечения слайдов из видео (защита в глубину)`

## Реализация (Вариант B — одна точка правды)
Файл `internal/upload/service.go`:
- Добавлена константа `videoSlideExtractionDisabled = true` с комментарием на русском
  (что и зачем, как вернуть фичу — установить `false`).
- Добавлена функция `extractSlidesEffective(requested bool) bool` — единая точка
  правды: при `videoSlideExtractionDisabled == true` всегда возвращает `false`,
  иначе — исходное значение.
- В `ConfirmFileUpload` и `CreateYouTube` вызов `noSlides(in.HasPDF, in.ExtractSlides)`
  заменён на `noSlides(in.HasPDF, extractSlidesEffective(in.ExtractSlides))`.
- Поля `ConfirmInput.ExtractSlides` / `YouTubeInput.ExtractSlides` оставлены без
  изменений (совместимость сигнатур и HTTP-парсинга формы).
- `handlers.go` не тронут: он только парсит `extract_slides` в `ConfirmInput`/
  `YouTubeInput.ExtractSlides` (`parseUploadCheckbox`), расчёт `noSlides` происходит
  только в `service.go` для обоих путей (file + youtube) — форсинг в одном месте
  покрывает оба.

## Тесты (TDD, RED → GREEN)

### service_test.go
- `TestConfirmFileUpload_ExtractOn` переименован в
  `TestConfirmFileUpload_ExtractOnStillForcesNoSlides`: было `want=false`, стало
  `want=true` (ExtractSlides:true всё равно даёт NoSlides:true).
- `TestCreateYouTube_Success`: инлайн-проверка `p.NoSlides` инвертирована —
  раньше ожидала `false`, теперь `true`.
- Добавлены:
  - `TestCreateYouTube_ExtractSlidesForcedOff` — youtube, ExtractSlides:true → NoSlides:true.
  - `TestCreateYouTube_PDFForcesNoSlides` — youtube, HasPDF:true → NoSlides:true.
  - `TestCreateYouTube_NoSlidesAtAll` — youtube, оба false → NoSlides:true.
  - Хелпер `assertYouTubeNoSlides` (аналог существовавшего `assertConfirmNoSlides`,
    но для youtube-пути) — проверяет параметр, реально дошедший до coreclient
    (`CreateTaskParams.NoSlides`), не тавтологичен.
- Существующие `TestConfirmFileUpload_PDFForcesNoSlides` и
  `TestConfirmFileUpload_ExtractToggleOff` не изменились (уже покрывали
  «has_pdf работает» и «без слайдов» → true).

### handlers_test.go
- `TestConfirm_Success`: инлайн-проверка `p.NoSlides` инвертирована (было
  `false`, стало `true`), т.к. форма шлёт `extract_slides=on`.
- `TestYouTube_Success`: добавлена явная проверка `p.NoSlides == true`
  (раньше NoSlides вообще не проверялся в этом тесте).
- Добавлен `TestConfirm_ExtractSlidesCheckedStillForcesNoSlides` — форма с
  `extract_slides=on` без `has_pdf` → `no_slides=true` в ядро (проверка через
  HTTP-хендлер до coreclient-параметра, не тавтология).
- `TestConfirm_HasPDFForcesNoSlides` и `TestConfirm_ExtractSlidesUnchecked` не
  менялись (уже покрывали нужные кейсы).

### Верификация RED
Перед правкой `service.go` прогнал `go test ./internal/upload/...` — упало 6
тестов ровно с ожидаемой причиной (`NoSlides = false, want true`), что
подтвердило: тесты действительно проверяют новое поведение, а не что-то ещё.

## Проверка
```
make vet   → чисто (go vet ./...)
make test  → все пакеты ok, включая internal/upload
```
Полный вывод: все 12 пакетов `ok`, внешние по отношению к upload — не задеты.

## Сомнения / concerns
- Название переменной задачи предлагало `noSlides(in.HasPDF, false)` буквально;
  вместо literal `false` я ввёл промежуточную функцию `extractSlidesEffective`,
  чтобы форсинг остался в одной именованной точке и читался явно на месте
  вызова (что ближе к духу «одна точка правды» из Варианта B). Семантически
  эквивалентно, при необходимости легко упростить обратно до `false` inline.
- coreclient и handlers.go не трогал — подтверждено, что оба пути (file и
  youtube) идут через `service.go`, форсинг в одном месте достаточен.
- Задачи 2/3 (шаблон, JS) вне этой задачи, не проверял/не трогал.

## Fix (review round 1): retry-путь

### Находка
`lecture.Retry` пересоздавал core-задачу через `CreateTaskParams{S3Key, VideoURL, Media}`
без `NoSlides` — retry видео-лекции снова запрашивал извлечение слайдов из видео,
обходя форсинг из upload-пути (b5c0586).

### Реализация
- `internal/lecture/lecture.go`: добавлено поле `NoSlides bool` в `CreateTaskParams`
  с комментарием, ссылающимся на upload-путь и причину дублирования.
- `internal/lecture/service.go`: добавлены константа `videoSlideExtractionDisabled = true`
  и функция `isVideoSource(lec Lecture) bool` (VideoURL != "" ИЛИ SourceKind
  in {"video", "video_url"}). В `Retry` — `NoSlides: videoSlideExtractionDisabled &&
  isVideoSource(*lec)`.
- Единая точка правды: константа `videoSlideExtractionDisabled` в `internal/upload`
  не экспортирована, а заводить публичный API пакета upload ради одной bool-константы
  избыточно (upload и lecture — независимые доменные пакеты, cross-import через
  внутренний identifier недопустим). Выбран вариант с дублированием именованной
  константы в `internal/lecture` с явным комментарием-ссылкой на upload/b5c0586 и
  инструкцией «выключать в обоих местах». Обратимость сохранена.
- `cmd/server/main.go` `coreTasksAdapter.CreateTask`: проброшено `NoSlides: p.NoSlides`
  в `coreclient.CreateTaskParams` (поле там уже существовало).

### Тесты (TDD, RED → GREEN)
- `TestService_Retry_VideoForcesNoSlides` — видео-лекция (SourceKind="video_url",
  VideoURL задан) → `NoSlides=true` доходит до fake core-клиента.
- `TestService_Retry_AudioDoesNotForceNoSlides` — аудио-лекция (S3Key, SourceKind="audio",
  VideoURL пуст) → `NoSlides=false` (регресс не сломан).
- Дополнена проверка в существующем `TestService_Retry_Success` (аудио-кейс) —
  явный assert `NoSlides == false`.
- RED подтверждён: до добавления поля `NoSlides` в `CreateTaskParams` тесты не
  компилировались (`lastCreateTaskParams.NoSlides undefined`).

### Проверка
`make vet && make test` — все пакеты зелёные, включая `internal/lecture`.

### Concerns
- Дублирование константы `videoSlideExtractionDisabled` в двух пакетах — при
  снятии форсинга нужно не забыть выключить в обоих местах (явно указано в
  комментариях обеих констант).
