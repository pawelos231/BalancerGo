import { Button } from '@mui/material';
import MenuBookOutlinedIcon from '@mui/icons-material/MenuBookOutlined';
import { Link as RouterLink, useLocation } from 'react-router';

// Both entry points open the same documentation view without leaving the app.
export default function DocumentationLink() {
  const active = useLocation().pathname.startsWith('/docs');

  return (
    <Button
      component={RouterLink} to="/docs" size="small"
      startIcon={<MenuBookOutlinedIcon />}
      aria-current={active ? 'page' : undefined}
      sx={{
        textTransform: 'none', whiteSpace: 'nowrap',
        bgcolor: active ? 'action.selected' : undefined,
      }}
    >
      View Docs
    </Button>
  );
}
