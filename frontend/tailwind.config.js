/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {},
    // Breakpoints do projeto. `md` = 721px para que `max-md:` case EXATAMENTE o breakpoint móvel
    // que o webmail.css já usa (max-width:720px) — sem zona morta entre os dois sistemas.
    screens: {
      sm: "640px",
      md: "721px",
      lg: "1024px",
      xl: "1280px",
    },
  },
  // A app inteira vive sob `.wm` (CSS de alta especificidade). `important: true` faz os utilitários
  // Tailwind ganharem essa disputa. É seguro porque a app quase não usava utilitários — logo não
  // altera o layout existente; só habilita os utilitários responsivos (max-md:*) que adicionamos.
  important: true,
  plugins: [],
};
