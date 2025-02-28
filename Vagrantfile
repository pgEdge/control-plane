# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  N = 3
  (1..N).each do |machine_id|
    config.vm.define "control-plane-#{machine_id}" do |machine|
      # The official rocky linux boxes are unusable on Apple Silicon because
      # they're built improperly for arm64 machines. These bento ones come from
      # the company that makes Chef, and this particular version is working
      # properly.
      machine.vm.box = "bento/rockylinux-9.3"
      machine.vm.box_version = "202404.23.0"
      machine.vm.hostname = "control-plane-#{machine_id}"
      machine.vm.network "private_network", ip: "10.1.0.#{10 + machine_id}"
      machine.vm.synced_folder ".", "/home/vagrant/control-plane"

      # Re-evaluate whether this is necessary whenever we upgrade the box.
      machine.vm.provider :vmware_desktop do |vmware|
        vmware.allowlist_verified = true
      end
    end
  end
end
