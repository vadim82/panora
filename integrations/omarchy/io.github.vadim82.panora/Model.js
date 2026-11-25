.pragma library

function executablePath(value) {
    return typeof value === "string" && value.startsWith("/") && value.length > 1
        && value.length <= 4096 && !/[\x00-\x1f\x7f]/.test(value) ? value : "";
}

function disconnected() {
    return {version: 1, connected: false, paused: true, busy: false,
        uncertain: false, setup_required: false, can_previous: false, warning: false};
}
function text(value, limit) {
    return typeof value === "string" ? value.replace(/[\x00-\x1f\x7f]/g, " ").slice(0, limit) : "";
}
function imageKey(snapshot) {
    return snapshot.connected && snapshot.image
        ? snapshot.instance_id + "/" + snapshot.image.id + "/" + snapshot.image.history_id : "";
}
function parse(raw) {
    if (typeof raw !== "string" || raw.length > 3 * 1024 * 1024) return null;
    var value;
    try { value = JSON.parse(raw); } catch (e) { return null; }
    if (!value || value.version !== 1 || typeof value.connected !== "boolean") return null;
    if (!value.connected) return disconnected();
    if (typeof value.instance_id !== "string" || !value.instance_id || value.instance_id.length > 128) return null;
    var result = {version: 1, connected: true, instance_id: value.instance_id};
    var flags = ["paused", "busy", "uncertain", "setup_required", "can_previous", "warning"];
    for (var i = 0; i < flags.length; i++) {
        if (typeof value[flags[i]] !== "boolean") return null;
        result[flags[i]] = value[flags[i]];
    }
    result.next_at = text(value.next_at, 40);
    if (value.image !== undefined) {
        var image = value.image;
        if (!image || typeof image.id !== "string" || !/^[1-9][0-9]{0,18}$/.test(image.id)
            || typeof image.history_id !== "string" || !image.history_id || image.history_id.length > 64
            || ["not_requested", "available", "unavailable", "invalid"].indexOf(image.preview_state) < 0) return null;
        result.image = {id: image.id, history_id: image.history_id, title: text(image.title, 256),
            origin: text(image.origin, 128), preview_state: image.preview_state,
            width: Number.isSafeInteger(image.width) && image.width > 0 ? image.width : 0,
            height: Number.isSafeInteger(image.height) && image.height > 0 ? image.height : 0};
        if (image.preview_state === "available") {
            if (typeof image.data_uri !== "string" || image.data_uri.length > 2800000
                || !/^data:image\/png;base64,[A-Za-z0-9+/]+={0,2}$/.test(image.data_uri)) return null;
            result.image.data_uri = image.data_uri;
        }
    }
    return result;
}
function allowed(snapshot, action) {
    if (!snapshot.connected) return false;
    if (action === "stop") return true;
    if (snapshot.busy || snapshot.setup_required) return false;
    if (action === "previous") return snapshot.can_previous;
    if (action === "next") return true;
    if (snapshot.uncertain) return false;
    if (action === "pause") return !snapshot.paused;
    if (action === "resume") return snapshot.paused;
    return false;
}
function stateKey(snapshot) {
    if (!snapshot.connected) return "disconnected";
    if (snapshot.busy) return "busy";
    if (snapshot.uncertain) return "uncertain";
    if (snapshot.setup_required) return "setup";
    if (snapshot.paused) return "paused";
    return "running";
}

function actionSucceeded(raw, instance, action) {
    if (typeof raw !== "string" || raw.length > 1024 * 1024) return false;
    try {
        var value = JSON.parse(raw);
        if (!value || value.version !== 1 || value.error || value.instance_id !== instance) return false;
        if (action === "stop") return value.stopping === true;
        return typeof value.paused === "boolean" && typeof value.busy === "boolean"
            && typeof value.uncertain === "boolean";
    } catch (e) { return false; }
}
