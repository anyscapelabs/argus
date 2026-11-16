type VoiceIconProps = {
  size?: number
  className?: string
}

export default function VoiceIcon({ size = 18, className }: VoiceIconProps) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className={className}
      aria-hidden
    >
      <path
        fill="currentColor"
        d="M12 2.5a1.5 1.5 0 0 1 1.493 1.356L13.5 4v16a1.5 1.5 0 0 1-2.993.144L10.5 20V4A1.5 1.5 0 0 1 12 2.5Zm-4 3A1.5 1.5 0 0 1 9.5 7v10a1.5 1.5 0 0 1-3 0V7A1.5 1.5 0 0 1 8 5.5Zm8 0A1.5 1.5 0 0 1 17.5 7v10a1.5 1.5 0 0 1-3 0V7A1.5 1.5 0 0 1 16 5.5Zm-12 3A1.5 1.5 0 0 1 5.5 10v4a1.5 1.5 0 0 1-3 0v-4A1.5 1.5 0 0 1 4 8.5Zm16 0a1.5 1.5 0 0 1 1.493 1.356L21.5 10v4a1.5 1.5 0 0 1-2.993.144L18.5 14v-4A1.5 1.5 0 0 1 20 8.5Z"
      />
    </svg>
  )
}
