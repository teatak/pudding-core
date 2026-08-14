import { ExternalLink, RefreshCw } from "@/components/icons";
import { useEffect, useState, type ReactNode } from "react";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import {
	getDesktopPermissions,
	openDesktopPermissionSettings,
	requestDesktopPermission,
	type DesktopPermission,
	type DesktopPermissionState,
} from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";

import { SETTINGS_NARROW_CONTENT_CLASS, SettingsSection } from "./shared";

const unavailableState: DesktopPermissionState = {
	supported: false,
	accessibility: false,
	screenRecording: false,
	desktopScreenRecording: false,
	camera: false,
	microphone: false,
};

export function PermissionsSettings() {
	const { t } = useI18n();
	const [state, setState] = useState<DesktopPermissionState>(unavailableState);
	const [loading, setLoading] = useState(true);
	const [failed, setFailed] = useState(false);
	const [pendingPermission, setPendingPermission] = useState<DesktopPermission | null>(null);

	async function refresh() {
		setLoading(true);
		setFailed(false);
		try {
			setState(await getDesktopPermissions());
		} catch {
			setState(unavailableState);
			setFailed(true);
		} finally {
			setLoading(false);
		}
	}

	async function requestPermission(permission: DesktopPermission) {
		setPendingPermission(permission);
		setFailed(false);
		try {
			const next = await requestDesktopPermission(permission);
			if (!next) {
				setFailed(true);
				return;
			}
			setState(next);
			if (!next[permission]) {
				await openDesktopPermissionSettings(permission);
			}
		} catch {
			setFailed(true);
		} finally {
			setPendingPermission(null);
		}
	}

	useEffect(() => {
		void refresh();
	}, []);

	return (
		<div className={SETTINGS_NARROW_CONTENT_CLASS}>
			<SettingsSection
				action={
					<Button disabled={loading} size="sm" type="button" variant="ghost" onClick={() => void refresh()}>
						{loading ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />}
						{t("common.refresh")}
					</Button>
				}
				title={t("settings.permissions.title")}
			>
				<p className="text-sm leading-6 text-muted-foreground">{t("settings.permissions.desc")}</p>
				{failed ? <p className="text-sm text-destructive">{t("settings.permissions.loadFailed")}</p> : null}
				{!loading && !state.supported ? <p className="text-sm text-muted-foreground">{t("settings.permissions.unavailable")}</p> : null}
				{state.supported ? (
					<div className="grid gap-5">
						<PermissionGroup title={t("settings.permissions.computerUseGroup")}>
							<PermissionRow
								allowed={state.accessibility}
								description={t("settings.permissions.accessibilityDesc")}
								label={t("settings.permissions.accessibility")}
								permission="accessibility"
								requesting={pendingPermission === "accessibility"}
								onRequest={requestPermission}
							/>
							<PermissionRow
								allowed={state.screenRecording}
								description={t("settings.permissions.screenRecordingDesc")}
								label={t("settings.permissions.screenRecording")}
								permission="screenRecording"
								requesting={pendingPermission === "screenRecording"}
								onRequest={requestPermission}
							/>
						</PermissionGroup>
						<PermissionGroup title={t("settings.permissions.mediaGroup")}>
							<PermissionRow
								allowed={state.desktopScreenRecording}
								description={t("settings.permissions.desktopScreenRecordingDesc")}
								label={t("settings.permissions.desktopScreenRecording")}
								permission="desktopScreenRecording"
								requestable={false}
								requesting={false}
								onRequest={requestPermission}
							/>
							<PermissionRow
								allowed={state.camera}
								description={t("settings.permissions.cameraDesc")}
								label={t("settings.permissions.camera")}
								permission="camera"
								requesting={pendingPermission === "camera"}
								onRequest={requestPermission}
							/>
							<PermissionRow
								allowed={state.microphone}
								description={t("settings.permissions.microphoneDesc")}
								label={t("settings.permissions.microphone")}
								permission="microphone"
								requesting={pendingPermission === "microphone"}
								onRequest={requestPermission}
							/>
						</PermissionGroup>
					</div>
				) : null}
			</SettingsSection>
		</div>
	);
}

function PermissionGroup({ children, title }: { children: ReactNode; title: string }) {
	return (
		<section className="grid gap-2">
			<h3 className="text-sm font-medium text-muted-foreground">{title}</h3>
			<div className="divide-y divide-border/70 rounded-lg border border-border/70">{children}</div>
		</section>
	);
}

function PermissionRow({
	allowed,
	description,
	label,
	onRequest,
	permission,
	requestable = true,
	requesting,
}: {
	allowed: boolean;
	description: string;
	label: string;
	onRequest: (permission: DesktopPermission) => Promise<void>;
	permission: DesktopPermission;
	requestable?: boolean;
	requesting: boolean;
}) {
	const { t } = useI18n();
	const required = !allowed;
	return (
		<div className="grid gap-3 p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
			<div className="grid gap-1">
				<div className="flex flex-wrap items-center gap-2">
					<span className="text-sm font-medium">{label}</span>
					<span className={cn("rounded-md px-2 py-0.5 text-xs", required ? "bg-warning/15 text-warning" : "bg-success/15 text-success")}>
						{allowed ? t("settings.permissions.allowed") : t("settings.permissions.required")}
					</span>
				</div>
				<span className="text-xs leading-5 text-muted-foreground">{description}</span>
			</div>
			<Button
				disabled={requesting}
				size="sm"
				type="button"
				variant="outline"
				onClick={() => void (allowed || !requestable ? openDesktopPermissionSettings(permission) : onRequest(permission))}
			>
				{requesting ? <Spinner className="size-3.5" /> : <ExternalLink className="size-3.5" />}
				{allowed || !requestable ? t("settings.permissions.openSettings") : t("settings.permissions.requestPermission")}
			</Button>
		</div>
	);
}
