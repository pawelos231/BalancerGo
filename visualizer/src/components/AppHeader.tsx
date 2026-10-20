import {
  AppBar,
  Chip,
  FormControlLabel,
  Stack,
  Switch,
  Toolbar,
  Typography,
} from '@mui/material';
import HelpTooltip from './HelpTooltip';

interface AppHeaderProps {
  connected: boolean;
  darkMode: boolean;
  onToggleDarkMode: () => void;
}

function AppHeader({ connected, darkMode, onToggleDarkMode }: AppHeaderProps) {
  return (
    <AppBar position="static" color="primary" enableColorOnDark>
      <Toolbar
        sx={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 2,
        }}
      >
        <Stack direction="row" spacing={2} alignItems="center">
          <FormControlLabel
            control={
              <Switch
                checked={darkMode}
                onChange={onToggleDarkMode}
                color="default"
              />
            }
            label="Dark mode"
            sx={{ color: 'inherit' }}
          />

          <Typography variant="h6" component="div">
            Load Balancer Simulator
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.5} alignItems="center">
          <Chip
            label={connected ? 'Connected' : 'Disconnected'}
            color={connected ? 'success' : 'default'}
            variant={connected ? 'filled' : 'outlined'}
          />

          <HelpTooltip label="Connection status">
            Status of this page's connection to the simulator. The page reconnects
            automatically. Reconnecting or refreshing the page does not start a
            new experiment.
          </HelpTooltip>
        </Stack>
      </Toolbar>
    </AppBar>
  );
}

export default AppHeader;
