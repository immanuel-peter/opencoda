/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ["class"],
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        surface: {
          DEFAULT: "#0c0f14",
          raised: "#141a22",
          border: "#1e2836",
        },
        accent: {
          DEFAULT: "#3dd6c3",
          muted: "#2a9d8f",
          glow: "#3dd6c340",
        },
        warn: "#f4a261",
        danger: "#e63946",
      },
      fontFamily: {
        display: ["var(--font-display)", "system-ui"],
        mono: ["var(--font-mono)", "monospace"],
      },
    },
  },
  plugins: [],
};
