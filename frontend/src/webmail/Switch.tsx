// Interruptor estilo iOS (on/off). Acessível: é um <button role=switch> com aria-checked, então
// funciona por teclado e leitor de ecrã — ao contrário de um <div> pintado.
export function Switch({ checked, onChange, disabled, label }: {
  checked: boolean;
  onChange: (on: boolean) => void;
  disabled?: boolean;
  label?: string;
}) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={label} disabled={disabled}
      className={"switch" + (checked ? " on" : "")} onClick={() => onChange(!checked)}>
      <span className="knob" />
    </button>
  );
}
