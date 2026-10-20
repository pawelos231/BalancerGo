import { IconButton, Tooltip } from '@mui/material';
import HelpOutlineIcon from '@mui/icons-material/HelpOutline';
import { useState } from 'react';

interface HelpTooltipProps {
  label: string;
  children: string;
}

function HelpTooltip({ label, children }: HelpTooltipProps) {
  const [open, setOpen] = useState(false);

  return (
    <Tooltip
      title={children}
      open={open}
      onOpen={() => setOpen(true)}
      onClose={() => setOpen(false)}
      describeChild
      arrow
      enterDelay={150}
      enterTouchDelay={0}
      leaveTouchDelay={8000}
      slotProps={{ tooltip: { sx: { maxWidth: 360, fontSize: '0.85rem', lineHeight: 1.5 } } }}
    >
      <IconButton
        type="button"
        size="small"
        aria-label={`Help: ${label}`}
        onClick={() => setOpen(true)}
        sx={{ color: 'text.secondary', p: 0.5 }}
      >
        <HelpOutlineIcon fontSize="small" />
      </IconButton>
    </Tooltip>
  );
}

export default HelpTooltip;
