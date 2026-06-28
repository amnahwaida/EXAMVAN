/* extension.js — EXAMVAN Gesture Blocker for GNOME 46+ */
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

let _handler = null;

export default class GestureBlocker {
    enable() {
        // Capture all events at stage level, block gestures + hide overview
        _handler = global.stage.connect('captured-event', (_actor, event) => {
            let type = event.type();

            // Block all touchpad gestures (3-finger swipe etc.)
            if (type === 15) { // Clutter.EventType.TOUCHPAD_GESTURE
                return true;
            }

            // Force-close overview if it appears
            if (Main.overview && Main.overview.visible) {
                Main.overview.hide();
                return true;
            }

            return false;
        });

        // Hide overview immediately if visible
        if (Main.overview && Main.overview.visible) {
            Main.overview.hide();
        }
    }

    disable() {
        if (_handler && global.stage) {
            global.stage.disconnect(_handler);
            _handler = null;
        }
    }
}
