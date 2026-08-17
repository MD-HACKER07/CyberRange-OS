import type { Config } from "tailwindcss";

// "Vault" design system — dark hacker-terminal aesthetic (spec Section 6).
const config: Config = {
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        black: {
          DEFAULT: "#0A0A0B",
          soft: "#17171A",
        },
        vault: {
          white: "#F5F3EE",
          gold: "#D4AF37",
          "gold-bright": "#F4CD5C",
          red: "#C41E3A",
          "red-deep": "#8B0000",
          slate: "#2E3440",
        },
      },
      fontFamily: {
        display: ["var(--font-display)", "Sora", "sans-serif"],
        body: ["var(--font-body)", "Inter", "sans-serif"],
        mono: ["var(--font-mono)", "JetBrains Mono", "monospace"],
      },
      boxShadow: {
        "glow-gold": "0 0 12px rgba(212,175,55,0.35)",
        "glow-red": "0 0 12px rgba(196,30,58,0.4)",
      },
      keyframes: {
        pulseGlow: {
          "0%,100%": { opacity: "1" },
          "50%": { opacity: "0.55" },
        },
        scan: {
          "0%": { backgroundPosition: "0 0" },
          "100%": { backgroundPosition: "0 100%" },
        },
      },
      animation: {
        "pulse-glow": "pulseGlow 2s ease-in-out infinite",
        scan: "scan 8s linear infinite",
      },
    },
  },
  plugins: [],
};
export default config;
