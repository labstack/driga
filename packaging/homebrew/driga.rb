# Template only: replace every placeholder, review, then copy
# to a separately managed tap. Never populate this from a dry run or prerelease.
class Driga < Formula
  desc "Watch local AI coding agent sessions from the terminal"
  homepage "https://github.com/labstack/driga"
  version "VERSION_PLACEHOLDER"
  license "Apache-2.0"

  depends_on "git"

  on_macos do
    on_arm do
      url "https://github.com/labstack/driga/releases/download/v#{version}/driga_#{version}_darwin_arm64.tar.gz"
      sha256 "DARWIN_ARM64_SHA256_PLACEHOLDER"
    end
    on_intel do
      url "https://github.com/labstack/driga/releases/download/v#{version}/driga_#{version}_darwin_amd64.tar.gz"
      sha256 "DARWIN_AMD64_SHA256_PLACEHOLDER"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/labstack/driga/releases/download/v#{version}/driga_#{version}_linux_arm64.tar.gz"
      sha256 "LINUX_ARM64_SHA256_PLACEHOLDER"
    end
    on_intel do
      url "https://github.com/labstack/driga/releases/download/v#{version}/driga_#{version}_linux_amd64.tar.gz"
      sha256 "LINUX_AMD64_SHA256_PLACEHOLDER"
    end
  end

  def install
    bin.install "driga"
  end

  test do
    assert_match "driga #{version}", shell_output("#{bin}/driga version")
  end
end
