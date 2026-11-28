class Panora < Formula
  desc "Wallpaper rotation and playback controls"
  homepage "https://github.com/vadim82/panora"
  url "https://github.com/vadim82/panora.git",
      tag:      "v0.8.2",
      revision: "bb61eaefe4977701aed0e773e3385496447c6d75"
  license "MIT"

  depends_on "corepack" => :build
  depends_on "go" => :build
  depends_on "node" => :build
  depends_on "python@3.14" => :build
  depends_on arch: :arm64
  depends_on :macos

  # The packager resolves the pinned Go toolchain and frontend dependencies.
  allow_network_access! :build
  skip_clean "libexec/Panora.app"

  def install
    go_version = (buildpath/"go.mod").read[/^go ([\d.]+)$/, 1]
    odie "Panora's source must pin a Go version in go.mod." unless go_version
    ENV["GOTOOLCHAIN"] = "go#{go_version}"
    goroot = Utils.safe_popen_read("go", "env", "GOROOT").chomp
    ENV.prepend_path "PATH", Pathname(goroot)/"bin"
    ENV["GOTOOLCHAIN"] = "local"

    bundle = buildpath/"build/Panora.app"
    system Formula["python@3.14"].opt_bin/"python3.14", "ui/package_macos.py",
           "--output", bundle, "--version", version.to_s
    libexec.install bundle
    bin.write_exec_script libexec/"Panora.app/Contents/MacOS/panora"
  end

  def caveats
    <<~EOS
      Open the native app with:
        open "#{opt_libexec}/Panora.app"

      Before uninstalling Panora, use Prepare to move or remove app
      in Settings → Startup → Background service, then quit the window.
    EOS
  end

  test do
    bundle = libexec/"Panora.app"
    system "/usr/bin/codesign", "--verify", "--deep", "--strict", bundle
    assert_match "Version: #{version}", shell_output("#{bundle}/Contents/MacOS/panora-client --version")
    assert_match "panora", shell_output("#{bin}/panora --help")
  end
end
