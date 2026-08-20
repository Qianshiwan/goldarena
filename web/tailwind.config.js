/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        gold: {
          DEFAULT: '#D4AF37',
          light: '#F0D060',
          dark: '#B8960F',
          50: '#FFFDF0',
          100: '#FFF8CC',
          200: '#FFED99',
          300: '#FFE066',
          400: '#F5D033',
          500: '#D4AF37',
          600: '#B8960F',
          700: '#8F7200',
          800: '#665200',
          900: '#3D3100',
        },
        dark: {
          DEFAULT: '#0F1923',
          50: '#1A2733',
          100: '#16222D',
          200: '#0F1923',
          300: '#0A1219',
          400: '#060C12',
        },
        red: {
          trade: '#EF4444',
        },
        green: {
          trade: '#22C55E',
        },
      },
      fontFamily: {
        mono: ['JetBrains Mono', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
}
