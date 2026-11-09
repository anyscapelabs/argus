import './argus.css'

type ArgusProps = {
  state?: 'idle' | 'thinking' | 'typing'
  size?: number
  still?: boolean
  className?: string
}

function Eye({ cx, cy }: { cx: number; cy: number }) {
  return (
    <g transform={`translate(${cx} ${cy}) rotate(-45)`}>
      <g className="ar-len">
        <g className="ar-lid">
          <rect className="ar-body" x="-52.5" y="-97.5" width="105" height="195" fill="#121214" />
          <circle className="ar-cap-a" cx="0" cy="-97.5" r="52.5" fill="#121214" />
          <circle className="ar-cap-b" cx="0" cy="97.5" r="52.5" fill="#121214" />
        </g>
      </g>
    </g>
  )
}

export default function Argus({ state = 'idle', size = 48, still = false, className = '' }: ArgusProps) {
  const cls = ['argus', still && 'ar-still', className].filter(Boolean).join(' ')
  return (
    <svg
      className={cls}
      data-state={state}
      width={size}
      height={size}
      viewBox="200 200 624 624"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="Argus"
    >
      <g className="ar-pose">
        <g className="ar-motion">
          <path
            d="M268.195 317.692C274.667 293.536 293.536 274.667 317.692 268.195L558.373 203.705C582.529 197.232 608.304 204.138 625.988 221.822L802.178 398.012C819.862 415.696 826.768 441.471 820.296 465.627L755.805 706.308C749.333 730.464 730.464 749.333 706.308 755.805L465.627 820.295C441.471 826.768 415.696 819.862 398.012 802.178L221.822 625.988C204.138 608.304 197.232 582.529 203.705 558.373L268.195 317.692Z"
            fill="white"
          />
          <g className="ar-gaze">
            <g className="ar-gm">
              <Eye cx={441.79} cy={582.71} />
              <Eye cx={583.21} cy={441.29} />
            </g>
          </g>
        </g>
      </g>
    </svg>
  )
}