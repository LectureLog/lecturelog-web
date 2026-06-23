/** @type {import('tailwindcss').Config} */
// Tailwind-конфиг «Читальный зал».
// Все цвета резолвятся в var(--token); хардкод-цвета в шаблонах запрещены.
module.exports = {
  // Источники для Purge — только templ-шаблоны и Go-файлы пакета web.
  content: [
    "./internal/web/**/*.templ",
    "./internal/web/**/*.go",
  ],
  theme: {
    extend: {
      // Цвета-токены: резолвируются в CSS-переменные, тёмная тема «бесплатно».
      colors: {
        bg:          "var(--bg)",
        "bg-2":      "var(--bg-2)",
        surface:     "var(--surface)",
        "surface-2": "var(--surface-2)",
        ink:         "var(--ink)",
        "text-2":    "var(--text-2)",
        "text-3":    "var(--text-3)",
        border:      "var(--border)",
        "border-2":  "var(--border-2)",
        accent:      "var(--accent)",
        "accent-2":  "var(--accent-2)",
        "accent-soft": "var(--accent-soft)",
        rail:        "var(--rail)",
        mark:        "var(--mark)",
        "mark-cur":  "var(--mark-cur)",
      },
      // Шрифты из design-токенов.
      fontFamily: {
        serif: ["var(--font-serif)", "Georgia", "serif"],
        sans:  ["var(--font-sans)", "system-ui", "sans-serif"],
      },
      // Радиусы из design-токенов.
      borderRadius: {
        card:  "var(--r-card)",
        slide: "var(--r-slide)",
        btn:   "var(--r-btn)",
        pill:  "var(--r-pill)",
        track: "var(--r-track)",
      },
      // Тень из design-токенов.
      boxShadow: {
        token: "var(--shadow)",
      },
      // Высота шапки: sticky 58px (STYLE_GUIDE §4).
      height: {
        topbar: "58px",
      },
    },
  },
  plugins: [],
};
