class Cojira < Formula
  desc "Agent-first Jira and Confluence automation CLI"
  homepage "https://github.com/notabhay/cojira"
  head "https://github.com/notabhay/cojira.git", branch: "main"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", "-trimpath", *std_go_args(ldflags: "-s -w -X github.com/notabhay/cojira/internal/version.Version=#{version}")
  end

  test do
    assert_match "cojira", shell_output("#{bin}/cojira --help")
  end
end
