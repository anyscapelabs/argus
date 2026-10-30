import { GoArrowRight } from 'react-icons/go'

type OnboardingProps = {
  onContinue: () => void
}

export default function Onboarding({ onContinue }: OnboardingProps) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
      <div className="flex items-center gap-3">
        <img src="/logo.png" alt="Argus" className="h-16 w-16" />
        <h1 className="text-5xl text-primary" style={{ fontFamily: "'Geist Pixel Square'" }}>Argus Agent</h1>
      </div>
      <p className="mt-4 max-w-sm text-xl font-medium text-primary">
        Your always on companion for everyday work, ready whenever you are.
      </p>
      <button
        type="button"
        onClick={onContinue}
        className="mt-6 flex cursor-pointer items-center gap-1 rounded-full bg-accent px-3.5 py-2 text-sm font-medium text-black transition-colors hover:bg-hover-accent"
      >
        Lets Go
        <GoArrowRight size={14} />
      </button>
    </div>
  )
}