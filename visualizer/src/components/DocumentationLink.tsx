import { Button } from '@mui/material';
import MenuBookOutlinedIcon from '@mui/icons-material/MenuBookOutlined';

// Both entry points share this destination until the dedicated docs are ready.
export default function DocumentationLink() {
  return (
    <Button
      href="https://github.com/pawelos231/BalancerGo/blob/main/README.md"
      target="_blank" rel="noopener noreferrer" size="small"
      startIcon={<MenuBookOutlinedIcon />}
      aria-label="View Docs (opens in a new tab)"
      sx={{ textTransform: 'none', whiteSpace: 'nowrap' }}
    >
      View Docs
    </Button>
  );
}
