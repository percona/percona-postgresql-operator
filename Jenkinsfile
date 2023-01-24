GKERegion='us-central1-a'

void CreateCluster(String CLUSTER_SUFFIX, String SUBNETWORK = CLUSTER_SUFFIX) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            NODES_NUM=3
            export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_SUFFIX}
            export USE_GKE_GCLOUD_AUTH_PLUGIN=True
            source $HOME/google-cloud-sdk/path.bash.inc
            ret_num=0
            while [ \${ret_num} -lt 15 ]; do
                ret_val=0
                gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
                gcloud config set project $GCP_PROJECT
                gcloud container clusters list --filter $CLUSTER_NAME-${CLUSTER_SUFFIX} --zone $GKERegion --format='csv[no-heading](name)' | xargs gcloud container clusters delete --zone $GKERegion --quiet || true
                gcloud container clusters create --zone $GKERegion $CLUSTER_NAME-${CLUSTER_SUFFIX} --cluster-version=1.21 --machine-type=n1-standard-4 --preemptible --num-nodes=\$NODES_NUM --network=jenkins-pg-vpc --subnetwork=jenkins-pg-${SUBNETWORK} --no-enable-autoupgrade --cluster-ipv4-cidr=/21 && \
                gcloud container clusters update --zone $GKERegion $CLUSTER_NAME-${CLUSTER_SUFFIX} --update-labels delete-cluster-after-hours=6 && \
                kubectl create clusterrolebinding cluster-admin-binding --clusterrole cluster-admin --user jenkins@"$GCP_PROJECT".iam.gserviceaccount.com || ret_val=\$?
                if [ \${ret_val} -eq 0 ]; then break; fi
                ret_num=\$((ret_num + 1))
            done
            if [ \${ret_num} -eq 15 ]; then
                gcloud container clusters list --filter $CLUSTER_NAME-${CLUSTER_SUFFIX} --zone $GKERegion --format='csv[no-heading](name)' | xargs gcloud container clusters delete --zone $GKERegion --quiet || true
                exit 1
            fi
        """
   }
}

void ShutdownCluster(String CLUSTER_SUFFIX) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_SUFFIX}
            export USE_GKE_GCLOUD_AUTH_PLUGIN=True
            source $HOME/google-cloud-sdk/path.bash.inc
            gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
            gcloud config set project $GCP_PROJECT
            gcloud container clusters delete --zone $GKERegion $CLUSTER_NAME-${CLUSTER_SUFFIX}
        """
   }
}

void DeleteOldClusters(String FILTER) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            if [ -f $HOME/google-cloud-sdk/path.bash.inc ]; then
                export USE_GKE_GCLOUD_AUTH_PLUGIN=True
                source $HOME/google-cloud-sdk/path.bash.inc
                gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
                gcloud config set project $GCP_PROJECT
                for GKE_CLUSTER in \$(gcloud container clusters list --format='csv[no-heading](name)' --filter="$FILTER"); do
                    GKE_CLUSTER_STATUS=\$(gcloud container clusters list --format='csv[no-heading](status)' --filter="\$GKE_CLUSTER")
                    retry=0
                    while [ "\$GKE_CLUSTER_STATUS" == "PROVISIONING" ]; do
                        echo "Cluster \$GKE_CLUSTER is being provisioned, waiting before delete."
                        sleep 10
                        GKE_CLUSTER_STATUS=\$(gcloud container clusters list --format='csv[no-heading](status)' --filter="\$GKE_CLUSTER")
                        let retry+=1
                        if [ \$retry -ge 60 ]; then
                            echo "Cluster \$GKE_CLUSTER to delete is being provisioned for too long. Skipping..."
                            break
                        fi
                    done
                    gcloud container clusters delete --async --zone $GKERegion --quiet \$GKE_CLUSTER || true
                done
            fi
        """
   }
}

void pushLogFile(String FILE_NAME) {
    LOG_FILE_PATH="e2e-tests/logs/${FILE_NAME}.log"
    LOG_FILE_NAME="${FILE_NAME}.log"
    echo "Push logfile $LOG_FILE_NAME file to S3!"
    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            S3_PATH=s3://percona-jenkins-artifactory-public/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 ls \$S3_PATH/${LOG_FILE_NAME} || :
            aws s3 cp --content-type text/plain --quiet ${LOG_FILE_PATH} \$S3_PATH/${LOG_FILE_NAME} || :
        """
    }
}

void pushArtifactFile(String FILE_NAME) {
    echo "Push $FILE_NAME file to S3!"

    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            touch ${FILE_NAME}
            S3_PATH=s3://percona-jenkins-artifactory/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 ls \$S3_PATH/${FILE_NAME} || :
            aws s3 cp --quiet ${FILE_NAME} \$S3_PATH/${FILE_NAME} || :
        """
    }
}

void popArtifactFile(String FILE_NAME) {
    echo "Try to get $FILE_NAME file from S3!"

    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            S3_PATH=s3://percona-jenkins-artifactory/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 cp --quiet \$S3_PATH/${FILE_NAME} ${FILE_NAME} || :
        """
    }
}

TestsReport = '| Test name  | Status |\r\n| ------------- | ------------- |'
testsReportMap  = [:]
testsResultsMap = [:]

void makeReport() {
    def wholeTestAmount=sh(script: 'ls e2e-tests/tests| wc -l', , returnStdout: true).trim()
    def startedTestAmount = testsReportMap.size()
    for ( test in testsReportMap ) {
        TestsReport = TestsReport + "\r\n| ${test.key} | ${test.value} |"
    }
    TestsReport = TestsReport + "\r\n| We run $startedTestAmount out of $wholeTestAmount|"
}

void setTestsresults() {
    testsResultsMap.each { file ->
        pushArtifactFile("${file.key}")
    }
}

void runTest(String TEST_NAME, String CLUSTER_SUFFIX) {
    def retryCount = 0
    waitUntil {
        def testUrl = "https://percona-jenkins-artifactory-public.s3.amazonaws.com/cloud-pg-operator/${env.GIT_BRANCH}/${env.GIT_SHORT_COMMIT}/${TEST_NAME}.log"
        try {
            echo "The $TEST_NAME test was started!"
            testsReportMap[TEST_NAME] = "[failed]($testUrl)"

            def FILE_NAME = "${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME"
            popArtifactFile("$FILE_NAME")

            timeout(time: 90, unit: 'MINUTES') {
                sh """
                    if [ -f "$FILE_NAME" ]; then
                        echo "Skipping $TEST_NAME test because it passed in previous run."
                    else
                        if [ ! -d "e2e-tests/logs" ]; then
                       		mkdir "e2e-tests/logs"
                        fi
                        export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_SUFFIX}
                        export PATH="$HOME/.krew/bin:$PATH"
                        source $HOME/google-cloud-sdk/path.bash.inc
                        set -o pipefail
                        time kubectl kuttl test --config ./e2e-tests/kuttl.yaml --test "^${TEST_NAME}\$" |& tee e2e-tests/logs/${TEST_NAME}.log
                    fi
                """
            }
            pushArtifactFile("$FILE_NAME")
            testsReportMap[TEST_NAME] = "[passed]($testUrl)"
            testsResultsMap["$FILE_NAME"] = 'passed'
            return true
        }
        catch (exc) {
            echo "The $TEST_NAME test was failed!"
            if (retryCount >= 2) {
                currentBuild.result = 'FAILURE'
                return true
            }
            retryCount++
            return false
        }
        finally {
            pushLogFile(TEST_NAME)
            echo "The $TEST_NAME test was finished!"
        }
    }
}

void prepareNode() {
    sh '''
        sudo yum install -y https://repo.percona.com/yum/percona-release-latest.noarch.rpm || true
        sudo percona-release enable-only tools
        sudo yum install -y percona-xtrabackup-80 jq | true
    '''

    sh '''
        if [ ! -d $HOME/google-cloud-sdk/bin ]; then
            rm -rf $HOME/google-cloud-sdk
            curl https://sdk.cloud.google.com | bash
        fi
        source $HOME/google-cloud-sdk/path.bash.inc
        gcloud components install alpha
        gcloud components install kubectl
        curl -fsSL https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
        curl -s -L https://github.com/openshift/origin/releases/download/v3.11.0/openshift-origin-client-tools-v3.11.0-0cbc58b-linux-64bit.tar.gz \
            | sudo tar -C /usr/local/bin --strip-components 1 --wildcards -zxvpf - '*/oc'
        curl -s -L https://github.com/mitchellh/golicense/releases/latest/download/golicense_0.2.0_linux_x86_64.tar.gz \
            | sudo tar -C /usr/local/bin --wildcards -zxvpf -
        sudo sh -c "curl -s -L https://github.com/mikefarah/yq/releases/download/v4.14.2/yq_linux_amd64 > /usr/local/bin/yq"
        sudo chmod +x /usr/local/bin/yq
        cd "$(mktemp -d)"
        OS="$(uname | tr '[:upper:]' '[:lower:]')"
        ARCH="$(uname -m | sed -e 's/x86_64/amd64/')"
        KREW="krew-${OS}_${ARCH}"
        curl -fsSLO "https://github.com/kubernetes-sigs/krew/releases/download/v0.4.3/${KREW}.tar.gz"
        tar zxvf "${KREW}.tar.gz"
        ./"${KREW}" install krew
        rm -f "${KREW}.tar.gz"
        export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"
        kubectl krew install kuttl
        printf "%s is installed" "$(kubectl kuttl --version)"
    '''
}

def skipBranchBuilds = true
if ( env.CHANGE_URL ) {
    skipBranchBuilds = false
}

pipeline {
    environment {
        CLOUDSDK_CORE_DISABLE_PROMPTS = 1
        CLEAN_NAMESPACE = 1
        OPERATOR_NS = 'pg-operator'
        GIT_SHORT_COMMIT = sh(script: 'git rev-parse --short HEAD', , returnStdout: true).trim()
        VERSION = "${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}"
        CLUSTER_NAME = sh(script: "echo jen-pg-${env.CHANGE_ID}-${GIT_SHORT_COMMIT}-${env.BUILD_NUMBER} | tr '[:upper:]' '[:lower:]'", , returnStdout: true).trim()
        AUTHOR_NAME  = sh(script: "echo ${CHANGE_AUTHOR_EMAIL} | awk -F'@' '{print \$1}'", , returnStdout: true).trim()
    }
    agent {
        label 'docker'
    }
    options {
        disableConcurrentBuilds(abortPrevious: true)
    }
    stages {
        stage('Prepare') {
            when {
                expression {
                    !skipBranchBuilds
                }
            }
            steps {
                prepareNode()
                script {
                    if ( AUTHOR_NAME == 'null' )  {
                        AUTHOR_NAME = sh(script: "git show -s --pretty=%ae | awk -F'@' '{print \$1}'", , returnStdout: true).trim()
                    }
                    for (comment in pullRequest.comments) {
                        println("Author: ${comment.user}, Comment: ${comment.body}")
                        if (comment.user.equals('JNKPercona')) {
                            println("delete comment")
                            comment.delete()
                        }
                    }
                }
                withCredentials([file(credentialsId: 'cloud-secret-file', variable: 'CLOUD_SECRET_FILE'), file(credentialsId: 'cloud-minio-secret-file', variable: 'CLOUD_MINIO_SECRET_FILE')]) {
                    sh '''
                        cp $CLOUD_SECRET_FILE ./e2e-tests/conf/cloud-secret.yml
                        cp $CLOUD_MINIO_SECRET_FILE ./e2e-tests/conf/cloud-secret-minio-gw.yml
                    '''
                }
                stash includes: "**", name: "sourceFILES"
                DeleteOldClusters("jen-pg-$CHANGE_ID")
            }
        }
        stage('Build docker image') {
            when {
                expression {
                    !skipBranchBuilds
                }
            }
            steps {
                withCredentials([usernamePassword(credentialsId: 'hub.docker.com', passwordVariable: 'PASS', usernameVariable: 'USER')]) {
                    sh '''
                        DOCKER_TAG=perconalab/percona-postgresql-operator:$VERSION
                        docker_tag_file='./results/docker/TAG'
                        mkdir -p $(dirname ${docker_tag_file})
                        echo ${DOCKER_TAG} > "${docker_tag_file}"
                            sg docker -c "
                                docker login -u '${USER}' -p '${PASS}'
                                export RELEASE=0
                                export IMAGE=\$DOCKER_TAG
                                ./e2e-tests/build
                                docker logout
                            "
                        sudo rm -rf ./build
                    '''
                }
                stash includes: 'results/docker/TAG', name: 'IMAGE'
                archiveArtifacts 'results/docker/TAG'
            }
        }
        stage('Check licenses') {
             when {
                 expression {
                     !skipBranchBuilds
                 }
             }
             parallel {
                 stage('GoLicenseDetector test') {
                     steps {
                         sh """
                             mkdir -p $WORKSPACE/src/github.com/percona
                             ln -s $WORKSPACE $WORKSPACE/src/github.com/percona/percona-postgresql-operator
                             sg docker -c "
                                 docker run \
                                     --rm \
                                     -v $WORKSPACE/src/github.com/percona/percona-postgresql-operator:/go/src/github.com/percona/percona-postgresql-operator \
                                     -w /go/src/github.com/percona/percona-postgresql-operator \
                                     -e GO111MODULE=on \
                                     golang:1.19 sh -c '
                                         go install github.com/google/go-licenses@latest;
                                         /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/postgres-operator \
                                             | cut -d , -f 3 \
                                             | sort -u \
                                             > go-licenses-new || :
                                     '
                             "
                             diff -u ./e2e-tests/license/compare/go-licenses go-licenses-new
                         """
                     }
                 }
                 stage('GoLicense test') {
                     steps {
                         sh '''
                             mkdir -p $WORKSPACE/src/github.com/percona
                             ln -s $WORKSPACE $WORKSPACE/src/github.com/percona/percona-postgresql-operator
                             sg docker -c "
                                 docker run \
                                     --rm \
                                     -v $WORKSPACE/src/github.com/percona/percona-postgresql-operator:/go/src/github.com/percona/percona-postgresql-operator \
                                     -w /go/src/github.com/percona/percona-postgresql-operator \
                                     -e GO111MODULE=on \
                                     golang:1.19 sh -c 'go build -v -o percona-postgresql-operator github.com/percona/percona-postgresql-operator/cmd/postgres-operator'
                             "
                         '''

                         withCredentials([string(credentialsId: 'GITHUB_API_TOKEN', variable: 'GITHUB_TOKEN')]) {
                             sh """
                                 golicense -plain ./percona-postgresql-operator \
                                     | grep -v 'license not found' \
                                     | sed -r 's/^[^ ]+[ ]+//' \
                                     | sort \
                                     | uniq \
                                     > golicense-new || true
                                 diff -u ./e2e-tests/license/compare/golicense golicense-new
                             """
                         }
                     }
                 }
             }
        }
        stage('Run E2E tests') {
            parallel {
                stage('E2E Sandbox') {
                    when {
                        expression {
                            !skipBranchBuilds
                        }
                    }
                    agent {
                        label 'docker'
                    }
                    steps {
                        prepareNode()
                        unstash "sourceFILES"
                        CreateCluster('sandbox')
                        runTest('init-deploy', 'sandbox')
                        runTest('demand-backup', 'sandbox')
                        runTest('start-from-backup', 'sandbox')
                        runTest('monitoring', 'sandbox')
                        ShutdownCluster('sandbox')
                    }
                }
            }
        }
    }
    post {
        always {
            script {
                setTestsresults()
                if (currentBuild.result != null && currentBuild.result != 'SUCCESS' && currentBuild.nextBuild == null) {
                    try {
                        slackSend channel: "@${AUTHOR_NAME}", color: '#FF0000', message: "[${JOB_NAME}]: build ${currentBuild.result}, ${BUILD_URL} owner: @${AUTHOR_NAME}"
                    }
                    catch (exc) {
                        slackSend channel: '#cloud-dev-ci', color: '#FF0000', message: "[${JOB_NAME}]: build ${currentBuild.result}, ${BUILD_URL} owner: @${AUTHOR_NAME}"
                    }
                }
                if (env.CHANGE_URL && currentBuild.nextBuild == null) {
                    for (comment in pullRequest.comments) {
                        println("Author: ${comment.user}, Comment: ${comment.body}")
                        if (comment.user.equals('JNKPercona')) {
                            println("delete comment")
                            comment.delete()
                        }
                    }
                    makeReport()
                    unstash 'IMAGE'
                    def IMAGE = sh(returnStdout: true, script: "cat results/docker/TAG").trim()
                    TestsReport = TestsReport + "\r\n\r\ncommit: ${env.CHANGE_URL}/commits/${env.GIT_COMMIT}\r\nimage: `${IMAGE}`\r\n"
                    pullRequest.comment(TestsReport)
                }
            }
            DeleteOldClusters("$CLUSTER_NAME")
            sh """
                sudo docker system prune -fa
                sudo rm -rf ./*
                sudo rm -rf $HOME/google-cloud-sdk
            """
            deleteDir()
        }
    }
}
