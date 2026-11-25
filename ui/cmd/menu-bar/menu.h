enum PanoraAction {
    PanoraOpen = 1,
    PanoraPrevious,
    PanoraPause,
    PanoraResume,
    PanoraNext,
    PanoraAbout,
    PanoraStop,
    PanoraStateMessage
};

int panoraIsMenuBundle(void);
void panoraRunMenu(void);
void panoraUpdateMenu(char *text, int previous, int pause, int paused, int next, int stop);
void panoraQuitMenu(void);
