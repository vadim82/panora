import QtQuick
import Quickshell
import "../io.github.vadim82.panora"

Scope {
    id: test
    property string scenario: Quickshell.env("PANORA_SHELL_TEST_SCENARIO")
    property bool clicked: false
    property int elapsed: 0
    Controller {
        id: controller
        executable: Quickshell.env("PANORA_SHELL_TEST_EXECUTABLE")
        uiExecutable: Quickshell.env("PANORA_SHELL_TEST_UI_EXECUTABLE")
    }
    Timer {
        interval: 50
        running: true
        repeat: true
        onTriggered: {
            test.elapsed += 50;
            if (["oversized", "stderr-oversized"].indexOf(test.scenario) >= 0 && controller.outputInvalid && !controller.observing) {
                console.log("PANORA_TEST " + JSON.stringify({pass: controller.response.length === 0 && !controller.snapshot.connected, length: controller.response.length}));
                Qt.quit();
            } else if (test.scenario === "timeout" && test.elapsed >= 7500 && !controller.observing) {
                console.log("PANORA_TEST " + JSON.stringify({pass: controller.outputInvalid && !controller.snapshot.connected}));
                Qt.quit();
            } else if (test.scenario === "incomplete" && test.elapsed > 300 && !controller.observing) {
                console.log("PANORA_TEST " + JSON.stringify({pass: !controller.snapshot.connected}));
                Qt.quit();
            } else if (test.scenario === "instance-change") {
                if (!test.clicked && controller.snapshot.connected && !controller.observing) {
                    test.clicked = true;
                    controller.act("next");
                } else if (test.clicked && !controller.actionInFlight && !controller.observing) {
                    console.log("PANORA_TEST " + JSON.stringify({pass: controller.snapshot.instance_id === "two" && controller.actionError === "actionUnconfirmed"}));
                    Qt.quit();
                }
            } else if (test.scenario === "action-malformed") {
                if (!test.clicked && controller.snapshot.connected && !controller.observing) {
                    test.clicked = true;
                    controller.act("next");
                } else if (test.clicked && !controller.actionInFlight && controller.actionError) {
                    console.log("PANORA_TEST " + JSON.stringify({pass: controller.actionError === "actionUnconfirmed"}));
                    Qt.quit();
                }
            } else if (test.scenario === "quit-desktop") {
                if (!test.clicked && controller.snapshot.connected && !controller.observing) {
                    test.clicked = true;
                    controller.act("stop");
                } else if (test.clicked && !controller.actionInFlight && !controller.observing) {
                    console.log("PANORA_TEST " + JSON.stringify({pass: controller.actionError === ""}));
                    Qt.quit();
                }
            }
            if (test.elapsed > (test.scenario === "timeout" ? 10000 : 4000)) {
                console.log("PANORA_TEST " + JSON.stringify({pass: false, reason: "timeout", scenario: test.scenario}));
                Qt.quit();
            }
        }
    }
}
