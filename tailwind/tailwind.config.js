/** @type {import('tailwindcss').Config} */
const defaultTheme = require("tailwindcss/defaultTheme");

module.exports = {
  content: [
    "./internal/view/**/*.templ",
  ],
  theme: {
    extend: {
      colors: {
        ff: {
          // Flint — warm charcoal neutrals (cool stone, faintly warmed)
          dark:    "#121010", // page background
          dark2:   "#1b1714", // glow base / secondary background
          panel:   "#191512", // card surface
          panel2:  "#241e18", // raised surface

          // Ember — the single warm spark accent
          ember:         "#db7b34",
          "ember-hover": "#ec8d44",
          "ember-mid":   "#6b4124", // muted ember: borders, dividers
          "ember-lo":    "#3a2417", // deep ember wash: bg tints, glow cores
          glow:    "#e8b483", // warm-gold light accent: links, focus, inline emphasis

          // Light text — warm whites, never pure white
          paper:   "#f5efe6", // brightest
          cream:   "#e7ddcf",
          moon:    "#d9c4a8", // warm secondary-bright
          ash:     "#948a7c", // muted secondary text
          stone:   "#4d463b", // faint / disabled

          // Hairlines — warm white alpha reads neutral on charcoal
          border:  "rgba(245,239,230,0.07)",
          border2: "rgba(245,239,230,0.13)",
        },
      },
      fontFamily: {
        display: ['"Cormorant Garamond"', ...defaultTheme.fontFamily.serif],
        body:    ['"DM Sans"', ...defaultTheme.fontFamily.sans],
      },
    },
  },
  plugins: [],
}
